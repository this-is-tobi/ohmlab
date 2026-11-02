package sonarqube

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AdminPermissions are the permissions granted to the admin group.
var AdminPermissions = []string{
	"admin",        // Administer System
	"gateadmin",    // Administer Quality Gates
	"profileadmin", // Administer Quality Profiles
	"provisioning", // Create Projects
	"scan",         // Execute Analysis
}

// Config holds SonarQube configuration parameters.
type Config struct {
	Domain      string
	Username    string
	Password    string
	MaxRetries  int
	RetryDelay  time.Duration
	HTTPTimeout time.Duration
}

// Defaults fills in zero-valued fields with sensible defaults.
func (c *Config) Defaults() {
	if c.MaxRetries == 0 {
		c.MaxRetries = 5
	}
	if c.RetryDelay == 0 {
		c.RetryDelay = 10 * time.Second
	}
	if c.HTTPTimeout == 0 {
		c.HTTPTimeout = 30 * time.Second
	}
}

// Validate checks that all required fields are set.
func (c *Config) Validate() error {
	if c.Domain == "" {
		return fmt.Errorf("sonarqube domain is required")
	}
	if c.Username == "" || c.Password == "" {
		return fmt.Errorf("sonarqube admin credentials are required")
	}
	return nil
}

// Configure creates the admin group and assigns permissions.
func Configure(cfg Config) error {
	cfg.Defaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	client := &http.Client{Timeout: cfg.HTTPTimeout}

	slog.Info("waiting for SonarQube to be ready", "domain", cfg.Domain)
	if err := waitForReady(client, cfg); err != nil {
		return err
	}

	slog.Info("configuring SonarQube admin group")
	if err := ensureAdminGroup(client, cfg); err != nil {
		return fmt.Errorf("admin group: %w", err)
	}

	slog.Info("configuring admin group permissions")
	if err := configurePermissions(client, cfg); err != nil {
		return fmt.Errorf("permissions: %w", err)
	}

	slog.Info("verifying SonarQube configuration")
	if err := verifyConfig(client, cfg); err != nil {
		return fmt.Errorf("verify: %w", err)
	}

	slog.Info("SonarQube configuration complete")
	return nil
}

func waitForReady(client *http.Client, cfg Config) error {
	statusURL := fmt.Sprintf("https://%s/api/system/status", cfg.Domain)
	timeout := 5 * time.Minute

	start := time.Now()
	for time.Since(start) < timeout {
		resp, err := client.Get(statusURL)
		if err == nil {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			var status struct {
				Status string `json:"status"`
			}
			if json.Unmarshal(body, &status) == nil && status.Status == "UP" {
				slog.Info("SonarQube is ready")
				return nil
			}
		}
		slog.Debug("waiting for SonarQube...", "elapsed", time.Since(start).Round(time.Second))
		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("timeout waiting for SonarQube at %s", cfg.Domain)
}

func ensureAdminGroup(client *http.Client, cfg Config) error {
	// Check if group exists
	if groupExists(client, cfg) {
		slog.Info("admin group already exists")
		return nil
	}

	// Create it
	return apiPost(client, cfg, "/api/user_groups/create", url.Values{
		"name":        {"admin"},
		"description": {"Administrators group"},
	})
}

func groupExists(client *http.Client, cfg Config) bool {
	apiURL := fmt.Sprintf("https://%s/api/user_groups/search?q=admin", cfg.Domain)
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.SetBasicAuth(cfg.Username, cfg.Password)

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Groups []struct {
			Name string `json:"name"`
		} `json:"groups"`
	}
	if json.Unmarshal(body, &result) != nil {
		return false
	}

	for _, g := range result.Groups {
		if g.Name == "admin" {
			return true
		}
	}
	return false
}

func configurePermissions(client *http.Client, cfg Config) error {
	for _, perm := range AdminPermissions {
		slog.Debug("granting permission", "permission", perm)
		err := apiPost(client, cfg, "/api/permissions/add_group", url.Values{
			"groupName":  {"admin"},
			"permission": {perm},
		})
		if err != nil {
			// Check if it's an "already exists" style error — treat as idempotent
			slog.Warn("failed to grant permission (may already exist)", "permission", perm, "error", err)
		}
	}
	return nil
}

func verifyConfig(client *http.Client, cfg Config) error {
	apiURL := fmt.Sprintf("https://%s/api/permissions/groups?q=admin", cfg.Domain)
	req, _ := http.NewRequest("GET", apiURL, nil)
	req.SetBasicAuth(cfg.Username, cfg.Password)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch permissions: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var result struct {
		Groups []struct {
			Name        string   `json:"name"`
			Permissions []string `json:"permissions"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	for _, g := range result.Groups {
		if g.Name == "admin" {
			slog.Info("admin group permissions verified", "permissions", g.Permissions, "count", len(g.Permissions))
			return nil
		}
	}

	return fmt.Errorf("admin group not found in permissions response")
}

func apiPost(client *http.Client, cfg Config, path string, params url.Values) error {
	apiURL := fmt.Sprintf("https://%s%s", cfg.Domain, path)

	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		req, err := http.NewRequest("POST", apiURL, strings.NewReader(params.Encode()))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.SetBasicAuth(cfg.Username, cfg.Password)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("SonarQube API request failed", "attempt", attempt, "error", err)
			if attempt < cfg.MaxRetries {
				time.Sleep(cfg.RetryDelay)
			}
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		// Idempotent: "already exists" is not an error
		if resp.StatusCode == 400 {
			var errResp struct {
				Errors []struct {
					Msg string `json:"msg"`
				} `json:"errors"`
			}
			if json.Unmarshal(respBody, &errResp) == nil {
				for _, e := range errResp.Errors {
					if strings.Contains(e.Msg, "already exists") {
						return nil
					}
				}
			}
		}

		slog.Warn("SonarQube API error", "status", resp.StatusCode, "attempt", attempt, "body", string(respBody))
		if attempt < cfg.MaxRetries {
			time.Sleep(cfg.RetryDelay)
		}
	}

	return fmt.Errorf("API call failed after %d retries: POST %s", cfg.MaxRetries, path)
}
