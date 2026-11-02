package harbor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Config holds the Harbor OIDC configuration parameters.
type Config struct {
	Domain         string
	Username       string
	Password       string
	ClientID       string
	ClientSecret   string
	KeycloakDomain string
	KeycloakRealm  string
	APIVersion     string
	MaxRetries     int
	RetryDelay     time.Duration
	HTTPTimeout    time.Duration
}

// Defaults fills in zero-valued fields with sensible defaults.
func (c *Config) Defaults() {
	if c.APIVersion == "" {
		c.APIVersion = "v2.0"
	}
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
		return fmt.Errorf("harbor domain is required")
	}
	if c.Username == "" || c.Password == "" {
		return fmt.Errorf("harbor admin credentials are required")
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return fmt.Errorf("harbor OIDC client credentials are required")
	}
	if c.KeycloakDomain == "" || c.KeycloakRealm == "" {
		return fmt.Errorf("keycloak domain and realm are required")
	}
	return nil
}

// Configure applies OIDC and Trivy settings to a Harbor instance.
func Configure(cfg Config) error {
	cfg.Defaults()
	if err := cfg.Validate(); err != nil {
		return err
	}

	client := &http.Client{Timeout: cfg.HTTPTimeout}

	slog.Info("waiting for Harbor to be ready", "domain", cfg.Domain)
	if err := waitForReady(client, cfg); err != nil {
		return err
	}

	slog.Info("configuring Harbor OIDC")
	if err := configureOIDC(client, cfg); err != nil {
		return fmt.Errorf("configure OIDC: %w", err)
	}

	slog.Info("configuring Trivy scan schedule")
	if err := configureTrivyScan(client, cfg); err != nil {
		return fmt.Errorf("configure Trivy: %w", err)
	}

	slog.Info("verifying Harbor configuration")
	if err := verifyConfig(client, cfg); err != nil {
		return fmt.Errorf("verify config: %w", err)
	}

	slog.Info("Harbor configuration complete")
	return nil
}

func waitForReady(client *http.Client, cfg Config) error {
	url := fmt.Sprintf("https://%s/api/%s/systeminfo", cfg.Domain, cfg.APIVersion)
	timeout := 5 * time.Minute

	start := time.Now()
	for time.Since(start) < timeout {
		req, _ := http.NewRequest("GET", url, nil)
		req.SetBasicAuth(cfg.Username, cfg.Password)

		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				slog.Info("Harbor is ready")
				return nil
			}
		}
		slog.Debug("waiting for Harbor...", "elapsed", time.Since(start).Round(time.Second))
		time.Sleep(10 * time.Second)
	}

	return fmt.Errorf("timeout waiting for Harbor at %s", cfg.Domain)
}

func configureOIDC(client *http.Client, cfg Config) error {
	oidcConfig := map[string]any{
		"auth_mode":                    "oidc_auth",
		"notification_enable":          true,
		"oidc_admin_group":             "admin",
		"oidc_auto_onboard":            true,
		"oidc_client_id":               cfg.ClientID,
		"oidc_endpoint":                fmt.Sprintf("https://%s/realms/%s", cfg.KeycloakDomain, cfg.KeycloakRealm),
		"oidc_extra_redirect_params":   "{}",
		"oidc_group_filter":            "",
		"oidc_groups_claim":            "groups",
		"oidc_name":                    "keycloak",
		"oidc_scope":                   "openid,profile,email,roles,groups",
		"oidc_user_claim":              "email",
		"project_creation_restriction": "adminonly",
		"quota_per_project_enable":     true,
		"read_only":                    false,
		"robot_name_prefix":            "robot$",
		"self_registration":            false,
		"oidc_client_secret":           cfg.ClientSecret,
	}

	return apiCall(client, cfg, "PUT", fmt.Sprintf("/api/%s/configurations", cfg.APIVersion), oidcConfig)
}

func configureTrivyScan(client *http.Client, cfg Config) error {
	schedule := map[string]any{
		"schedule": map[string]any{
			"type": "Daily",
			"cron": "0 0 0 * * *",
		},
	}

	url := fmt.Sprintf("https://%s/api/%s/system/scanAll/schedule", cfg.Domain, cfg.APIVersion)
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(cfg.Username, cfg.Password)

	resp, err := client.Do(req)
	if err == nil {
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		var existing map[string]any
		if json.Unmarshal(body, &existing) == nil {
			if _, ok := existing["schedule"]; ok {
				slog.Info("Trivy scan schedule already configured")
				return nil
			}
		}
	}

	return apiCall(client, cfg, "POST", fmt.Sprintf("/api/%s/system/scanAll/schedule", cfg.APIVersion), schedule)
}

func verifyConfig(client *http.Client, cfg Config) error {
	url := fmt.Sprintf("https://%s/api/%s/configurations", cfg.Domain, cfg.APIVersion)
	req, _ := http.NewRequest("GET", url, nil)
	req.SetBasicAuth(cfg.Username, cfg.Password)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch config: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var config map[string]any
	if err := json.Unmarshal(body, &config); err != nil {
		return fmt.Errorf("decode config: %w", err)
	}

	authMode, _ := config["auth_mode"].(map[string]any)
	if v, _ := authMode["value"].(string); v != "oidc_auth" {
		return fmt.Errorf("auth_mode is %q, expected oidc_auth", v)
	}

	slog.Info("Harbor configuration verified", "auth_mode", "oidc_auth")
	return nil
}

func apiCall(client *http.Client, cfg Config, method, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal body: %w", err)
	}

	url := fmt.Sprintf("https://%s%s", cfg.Domain, path)

	for attempt := 1; attempt <= cfg.MaxRetries; attempt++ {
		req, err := http.NewRequest(method, url, bytes.NewReader(data))
		if err != nil {
			return fmt.Errorf("create request: %w", err)
		}
		req.SetBasicAuth(cfg.Username, cfg.Password)
		req.Header.Set("Content-Type", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("Harbor API request failed", "attempt", attempt, "error", err)
			if attempt < cfg.MaxRetries {
				time.Sleep(cfg.RetryDelay)
			}
			continue
		}
		resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}

		slog.Warn("Harbor API returned error", "status", resp.StatusCode, "attempt", attempt)
		if attempt < cfg.MaxRetries {
			time.Sleep(cfg.RetryDelay)
		}
	}

	return fmt.Errorf("API call failed after %d retries: %s %s", cfg.MaxRetries, method, path)
}
