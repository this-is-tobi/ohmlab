package vault

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// AuthMethod defines supported Vault authentication methods.
//
// For multi-cluster deployments:
//   - JWT is recommended: uses K8s SA tokens validated via JWKS (no cross-cluster
//     API calls, automatic token rotation, one auth mount per cluster).
//   - AppRole works but requires distributing secret_id credentials to each
//     namespace and managing their rotation — more operational burden.
//   - Kubernetes auth only works same-cluster (Vault calls the K8s API directly).
type AuthMethod string

const (
	AuthKubernetes AuthMethod = "kubernetes"
	AuthJWT        AuthMethod = "jwt"
	AuthAppRole    AuthMethod = "approle"
	AuthToken      AuthMethod = "token"
)

// Client wraps the Vault HTTP API for KV-v2 operations.
type Client struct {
	addr   string
	token  string
	client *http.Client
	// session serves the context-aware API (kv2.go); the token above serves
	// the original one.
	session *session
}

// ClientConfig holds configuration for creating a Vault client.
type ClientConfig struct {
	Address       string
	CACertPath    string
	SkipTLSVerify bool
	Timeout       time.Duration
}

// NewClient creates a Vault client with the given TLS configuration.
func NewClient(cfg ClientConfig) (*Client, error) {
	tlsCfg := &tls.Config{
		InsecureSkipVerify: cfg.SkipTLSVerify, //nolint:gosec // user-controlled flag for internal clusters
	}

	if cfg.CACertPath != "" && !cfg.SkipTLSVerify {
		caCert, err := os.ReadFile(cfg.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("read CA cert %s: %w", cfg.CACertPath, err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA cert from %s", cfg.CACertPath)
		}
		tlsCfg.RootCAs = pool
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		addr: strings.TrimRight(cfg.Address, "/"),
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				TLSClientConfig: tlsCfg,
			},
		},
	}, nil
}

// Authenticate obtains a Vault token using the specified auth method.
func (c *Client) Authenticate(method AuthMethod, mount string, params map[string]string) error {
	switch method {
	case AuthKubernetes:
		return c.authKubernetes(mount, params)
	case AuthJWT:
		return c.authJWT(mount, params)
	case AuthAppRole:
		return c.authAppRole(mount, params)
	case AuthToken:
		return c.authToken(params)
	default:
		return fmt.Errorf("unsupported auth method: %s", method)
	}
}

// authKubernetes authenticates using a Kubernetes service account token.
// The SA token is read from the standard projected volume path.
func (c *Client) authKubernetes(mount string, params map[string]string) error {
	role := params["role"]
	if role == "" {
		return fmt.Errorf("kubernetes auth requires 'role' parameter")
	}

	tokenPath := params["token_path"]
	if tokenPath == "" {
		tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	jwt, err := os.ReadFile(tokenPath)
	if err != nil {
		return fmt.Errorf("read SA token from %s: %w", tokenPath, err)
	}

	if mount == "" {
		mount = "kubernetes"
	}

	body := map[string]string{
		"role": role,
		"jwt":  string(jwt),
	}

	return c.doAuthLogin(mount, body)
}

// authJWT authenticates using a JWT token (recommended for multi-cluster).
// Uses the same SA token as kubernetes auth, but Vault validates it via JWKS
// instead of calling the K8s API — no cross-cluster network dependency.
func (c *Client) authJWT(mount string, params map[string]string) error {
	role := params["role"]
	if role == "" {
		return fmt.Errorf("jwt auth requires 'role' parameter")
	}

	tokenPath := params["token_path"]
	if tokenPath == "" {
		tokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	}

	jwt, err := os.ReadFile(tokenPath)
	if err != nil {
		return fmt.Errorf("read JWT from %s: %w", tokenPath, err)
	}

	if mount == "" {
		mount = "jwt"
	}

	body := map[string]string{
		"role": role,
		"jwt":  string(jwt),
	}

	return c.doAuthLogin(mount, body)
}

// authAppRole authenticates using AppRole credentials.
// Requires role_id (static) and secret_id (dynamic, must be distributed to pods).
// Less recommended than JWT for multi-cluster: requires secret_id lifecycle
// management (distribution, rotation, TTL) whereas JWT uses existing SA tokens.
func (c *Client) authAppRole(mount string, params map[string]string) error {
	roleID := params["role_id"]
	secretID := params["secret_id"]
	if roleID == "" || secretID == "" {
		return fmt.Errorf("approle auth requires 'role_id' and 'secret_id' parameters")
	}

	if mount == "" {
		mount = "approle"
	}

	body := map[string]string{
		"role_id":   roleID,
		"secret_id": secretID,
	}

	return c.doAuthLogin(mount, body)
}

// authToken uses a pre-existing Vault token directly.
func (c *Client) authToken(params map[string]string) error {
	token := params["token"]
	if token == "" {
		token = os.Getenv("VAULT_TOKEN")
	}
	if token == "" {
		return fmt.Errorf("token auth requires 'token' parameter or VAULT_TOKEN env var")
	}
	c.token = token
	slog.Info("authenticated to Vault using token")
	return nil
}

func (c *Client) doAuthLogin(mount string, body map[string]string) error {
	path := fmt.Sprintf("/v1/auth/%s/login", mount)

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal auth body: %w", err)
	}

	resp, err := c.rawRequest("POST", path, data)
	if err != nil {
		return fmt.Errorf("auth login to %s: %w", mount, err)
	}

	var result struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("decode auth response: %w", err)
	}

	if result.Auth.ClientToken == "" {
		return fmt.Errorf("auth login returned empty token (mount=%s)", mount)
	}

	c.token = result.Auth.ClientToken
	slog.Info("authenticated to Vault", "mount", mount)
	return nil
}

// KVGet reads a secret from a KV-v2 engine.
// Returns the data map (the nested .data.data from the API response).
func (c *Client) KVGet(mount, path string) (map[string]any, error) {
	apiPath := fmt.Sprintf("/v1/%s/data/%s", mount, path)

	resp, err := c.rawRequest("GET", apiPath, nil)
	if err != nil {
		// 404 means the secret doesn't exist
		if strings.Contains(err.Error(), "status 404") {
			return nil, nil
		}
		return nil, fmt.Errorf("KV get %s/%s: %w", mount, path, err)
	}

	var result struct {
		Data struct {
			Data map[string]any `json:"data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("decode KV response for %s/%s: %w", mount, path, err)
	}

	return result.Data.Data, nil
}

// KVPut writes a secret to a KV-v2 engine.
func (c *Client) KVPut(mount, path string, data map[string]any) error {
	apiPath := fmt.Sprintf("/v1/%s/data/%s", mount, path)

	body := map[string]any{
		"data": data,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal KV data: %w", err)
	}

	_, err = c.rawRequest("POST", apiPath, payload)
	if err != nil {
		return fmt.Errorf("KV put %s/%s: %w", mount, path, err)
	}

	return nil
}

// Health checks if Vault is initialized and unsealed.
func (c *Client) Health() error {
	resp, err := c.rawRequest("GET", "/v1/sys/health", nil)
	if err != nil {
		// Health endpoint returns non-200 for sealed/standby but still valid JSON
		if resp != nil {
			var health struct {
				Initialized bool `json:"initialized"`
				Sealed      bool `json:"sealed"`
			}
			if json.Unmarshal(resp, &health) == nil {
				if !health.Initialized {
					return fmt.Errorf("vault is not initialized")
				}
				if health.Sealed {
					return fmt.Errorf("vault is sealed")
				}
			}
		}
		return fmt.Errorf("vault health check: %w", err)
	}
	return nil
}

// IdentityCreateGroup creates or updates an identity group with the given type and policies.
// groupType must be "internal" or "external"; defaults to "internal" if empty.
// Idempotent: if the group exists with the correct type it is updated in-place;
// if it exists with the wrong type it is deleted and recreated (type is immutable in Vault).
func (c *Client) IdentityCreateGroup(name, groupType string, policies []string) (string, error) {
	if groupType == "" {
		groupType = "internal"
	}

	// Check whether the group already exists and whether its type matches.
	existing, err := c.GetIdentityGroupByName(name)
	if err != nil {
		return "", fmt.Errorf("check existing group %s: %w", name, err)
	}
	if existing != nil && existing.Type != groupType {
		slog.Warn("group exists with wrong type, deleting and recreating",
			"name", name, "current_type", existing.Type, "desired_type", groupType)
		delPath := fmt.Sprintf("/v1/identity/group/id/%s", existing.ID)
		if _, err := c.rawRequest("DELETE", delPath, nil); err != nil {
			return "", fmt.Errorf("delete group %s for recreation: %w", name, err)
		}
	}

	path := "/v1/identity/group"
	body := map[string]any{
		"name":     name,
		"type":     groupType,
		"policies": policies,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", fmt.Errorf("marshal group data: %w", err)
	}

	resp, err := c.rawRequest("POST", path, payload)
	if err != nil {
		return "", fmt.Errorf("create group %s: %w", name, err)
	}

	// Vault sometimes returns 200 with data, sometimes 204/empty — handle both.
	if len(resp) == 0 {
		slog.Info("created/updated identity group (no response body)", "name", name, "type", groupType, "policies", policies)
		g, err := c.GetIdentityGroupByName(name)
		if err != nil || g == nil {
			return "", fmt.Errorf("retrieve group ID for %s after creation: %w", name, err)
		}
		slog.Info("retrieved group ID after creation", "name", name, "id", g.ID)
		return g.ID, nil
	}

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("decode group response: %w", err)
	}

	if result.Data.ID == "" {
		return "", fmt.Errorf("group creation returned empty ID for %s", name)
	}

	slog.Info("created/updated identity group", "name", name, "type", groupType, "id", result.Data.ID, "policies", policies)
	return result.Data.ID, nil
}

// IdentityCreateGroupAlias creates or updates a group alias mapping an external
// identity claim (e.g., OIDC group claim) to an external Vault group.
// Idempotent: skips if alias with same name+mount already exists, updates if
// the name differs, creates if no alias exists yet.
func (c *Client) IdentityCreateGroupAlias(name, groupID, mountAccessor string) error {
	// Check existing alias on this group by reading identity/group/id/{id}
	groupPath := fmt.Sprintf("/v1/identity/group/id/%s", groupID)
	groupResp, err := c.rawRequest("GET", groupPath, nil)
	if err != nil {
		return fmt.Errorf("read group %s for alias check: %w", groupID, err)
	}

	var groupData struct {
		Data struct {
			Alias *struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				MountAccessor string `json:"mount_accessor"`
			} `json:"alias"`
		} `json:"data"`
	}
	if err := json.Unmarshal(groupResp, &groupData); err != nil {
		return fmt.Errorf("decode group alias data: %w", err)
	}

	existingAlias := groupData.Data.Alias

	// Already correct — nothing to do.
	if existingAlias != nil && existingAlias.Name == name && existingAlias.MountAccessor == mountAccessor {
		slog.Info("group-alias already exists and is correct, skipping",
			"name", name, "group_id", groupID, "mount_accessor", mountAccessor)
		return nil
	}

	// Alias exists but with different values — update via PUT.
	if existingAlias != nil {
		slog.Info("updating existing group-alias",
			"alias_id", existingAlias.ID, "old_name", existingAlias.Name, "new_name", name)
		updatePath := fmt.Sprintf("/v1/identity/group-alias/id/%s", existingAlias.ID)
		body := map[string]string{
			"name":           name,
			"canonical_id":   groupID,
			"mount_accessor": mountAccessor,
		}
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal group-alias update: %w", err)
		}
		if _, err := c.rawRequest("POST", updatePath, payload); err != nil {
			return fmt.Errorf("update group-alias %s: %w", name, err)
		}
		slog.Info("updated group-alias", "name", name, "group_id", groupID, "mount_accessor", mountAccessor)
		return nil
	}

	// No alias yet — create.
	body := map[string]string{
		"name":           name,
		"canonical_id":   groupID,
		"mount_accessor": mountAccessor,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal group-alias data: %w", err)
	}
	if _, err = c.rawRequest("POST", "/v1/identity/group-alias", payload); err != nil {
		return fmt.Errorf("create group-alias %s: %w", name, err)
	}

	slog.Info("created group-alias", "name", name, "group_id", groupID, "mount_accessor", mountAccessor)
	return nil
}

// GetAuthMountAccessor retrieves the accessor ID for an auth mount by type and path.
// If path is empty, returns the first matching auth mount of that type.
// Returns empty string if mount not found.
func (c *Client) GetAuthMountAccessor(authType, authPath string) (string, error) {
	path := "/v1/sys/auth"
	resp, err := c.rawRequest("GET", path, nil)
	if err != nil {
		return "", fmt.Errorf("list auth mounts: %w", err)
	}

	type AuthMount struct {
		Accessor string `json:"accessor"`
		Type     string `json:"type"`
		Path     string `json:"path"`
	}

	type AuthMounts map[string]AuthMount

	var result struct {
		Data AuthMounts `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("decode auth mounts response: %w", err)
	}

	// If specific path provided, return its accessor
	if authPath != "" {
		// Normalize path (add trailing slash if needed)
		if !strings.HasSuffix(authPath, "/") {
			authPath += "/"
		}
		if mount, ok := result.Data[authPath]; ok && mount.Type == authType {
			return mount.Accessor, nil
		}
		return "", fmt.Errorf("auth mount not found: type=%s, path=%s", authType, authPath)
	}

	// Find first matching auth type
	for path, mount := range result.Data {
		if mount.Type == authType {
			slog.Info("found auth mount", "type", authType, "path", path, "accessor", mount.Accessor)
			return mount.Accessor, nil
		}
	}

	return "", fmt.Errorf("no auth mount found of type %s", authType)
}

// GetLeaderAddress discovers the active Vault leader node address by querying /v1/sys/leader.
// Returns the leader's address URL, or empty string if discovery fails.
// No authentication required for this endpoint.
func (c *Client) GetLeaderAddress() (string, error) {
	path := "/v1/sys/leader"
	resp, err := c.rawRequest("GET", path, nil)
	if err != nil {
		return "", fmt.Errorf("query leader endpoint: %w", err)
	}

	var result struct {
		LeaderAddress string `json:"leader_address"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return "", fmt.Errorf("decode leader response: %w", err)
	}

	if result.LeaderAddress == "" {
		return "", fmt.Errorf("no active leader found")
	}

	return result.LeaderAddress, nil
}

// IdentityGroupAlias holds the key fields of a group's existing alias.
type IdentityGroupAlias struct {
	ID            string
	Name          string
	MountAccessor string
}

// IdentityGroup holds the key fields of an existing Vault identity group.
type IdentityGroup struct {
	ID    string
	Type  string
	Alias *IdentityGroupAlias // nil if no alias exists
}

// GetIdentityGroupByName retrieves a group's ID, type, and alias by name.
// Returns nil (no error) when the group does not exist.
func (c *Client) GetIdentityGroupByName(name string) (*IdentityGroup, error) {
	path := fmt.Sprintf("/v1/identity/group/name/%s", url.QueryEscape(name))
	resp, err := c.rawRequest("GET", path, nil)
	if err != nil {
		// 404 means group doesn't exist yet — not an error
		if strings.Contains(err.Error(), "status 404") {
			return nil, nil
		}
		return nil, fmt.Errorf("get group by name %s: %w", name, err)
	}

	var result struct {
		Data struct {
			ID    string `json:"id"`
			Type  string `json:"type"`
			Alias *struct {
				ID            string `json:"id"`
				Name          string `json:"name"`
				MountAccessor string `json:"mount_accessor"`
			} `json:"alias"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("decode group response: %w", err)
	}

	if result.Data.ID == "" {
		return nil, fmt.Errorf("group %s has no ID", name)
	}

	g := &IdentityGroup{ID: result.Data.ID, Type: result.Data.Type}
	if result.Data.Alias != nil && result.Data.Alias.ID != "" {
		g.Alias = &IdentityGroupAlias{
			ID:            result.Data.Alias.ID,
			Name:          result.Data.Alias.Name,
			MountAccessor: result.Data.Alias.MountAccessor,
		}
	}
	return g, nil
}

func (c *Client) rawRequest(method, path string, body []byte) ([]byte, error) {
	// Retry logic for transient errors
	const maxRetries = 3
	backoff := time.Millisecond * 100

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			slog.Debug("retrying request after backoff", "attempt", attempt, "delay", backoff)
			time.Sleep(backoff)
			backoff = time.Duration(float64(backoff) * 2) // exponential backoff
		}

		url := c.addr + path

		var bodyReader io.Reader
		if body != nil {
			bodyReader = bytes.NewReader(body)
		}

		req, err := http.NewRequest(method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}

		if c.token != "" {
			req.Header.Set("X-Vault-Token", c.token)
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
		}
		defer resp.Body.Close()

		respBody, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("read response body: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			// 429 is retryable (transient rate-limiting)
			if resp.StatusCode == 429 && attempt < maxRetries-1 {
				slog.Debug("got 429, will retry", "attempt", attempt+1)
				continue
			}

			// Try to extract Vault error messages
			var vaultErr struct {
				Errors []string `json:"errors"`
			}
			if json.Unmarshal(respBody, &vaultErr) == nil && len(vaultErr.Errors) > 0 {
				return respBody, fmt.Errorf("status %d: %s", resp.StatusCode, strings.Join(vaultErr.Errors, "; "))
			}
			return respBody, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
		}

		return respBody, nil
	}

	return nil, fmt.Errorf("max retries exceeded")
}
