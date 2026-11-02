package vault

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// Typed failures of the context-aware API below.
var (
	// ErrCASMismatch is a compare-and-set write that lost a race: the path
	// changed since it was read.
	ErrCASMismatch = errors.New("check-and-set parameter did not match the current version")
	// ErrForbidden is a 403 that a fresh login did not cure: the policy does
	// not allow the operation.
	ErrForbidden = errors.New("permission denied")
)

const (
	// maxResponseBytes bounds what one reply may hold.
	maxResponseBytes = 8 << 20
	// serviceAccountToken is where Kubernetes projects the pod's token.
	serviceAccountToken = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

// KVSecret is one KV v2 path as the controller sees it.
type KVSecret struct {
	// Exists is false when the path was never written or its latest version is
	// deleted.
	Exists bool
	// Version is the latest version number, kept for a deleted path too: it is
	// the compare-and-set value of the next write. 0 means never written.
	Version  int64
	Data     map[string]any
	Metadata map[string]string
}

// session keeps a token alive for a Client: it logs in when there is none,
// logs in again before the token is out of time, and once more when Vault
// answers 403 (a revoked token reads exactly like a refused policy).
type session struct {
	mu     sync.Mutex
	login  func(ctx context.Context) (token string, ttl time.Duration, err error)
	token  string
	until  time.Time // when to log in again: two thirds into the token's life
	static bool      // a token supplied from outside is never renewed
}

// UseKubernetesLogin makes the client log in with the pod's service account.
// mount defaults to "kubernetes", tokenPath to the projected token.
func (c *Client) UseKubernetesLogin(mount, role, tokenPath string) {
	if mount == "" {
		mount = "kubernetes"
	}
	if tokenPath == "" {
		tokenPath = serviceAccountToken
	}
	c.UseLogin(func(ctx context.Context) (string, time.Duration, error) {
		jwt, err := os.ReadFile(tokenPath)
		if err != nil {
			return "", 0, fmt.Errorf("read service account token: %w", err)
		}
		return c.login(ctx, mount, map[string]string{"role": role, "jwt": strings.TrimSpace(string(jwt))})
	})
}

// UseLogin makes the client obtain its token from login, which returns the
// token and how long it lives. The client calls it when it has no token, when
// the token is two thirds spent, and after a 403.
func (c *Client) UseLogin(login func(ctx context.Context) (token string, ttl time.Duration, err error)) {
	c.session = &session{login: login}
}

// UseToken makes the client send a fixed token. It is for tests and tools.
func (c *Client) UseToken(token string) {
	c.session = &session{token: token, static: true}
}

// login posts credentials to an auth mount and returns the token it grants.
func (c *Client) login(ctx context.Context, mount string, body map[string]string) (string, time.Duration, error) {
	status, resp, err := c.send(ctx, http.MethodPost, fmt.Sprintf("/v1/auth/%s/login", mount), body, "")
	if err != nil {
		return "", 0, fmt.Errorf("login to %s: %w", mount, err)
	}
	// A role that does not exist, or that refuses this ServiceAccount, is the
	// first thing met while the platform is still being set up: say so.
	if status == http.StatusForbidden {
		return "", 0, fmt.Errorf("login to %s: %w: %s", mount, ErrForbidden, vaultMessage(resp))
	}
	if err := apiError("login to "+mount, status, resp); err != nil {
		return "", 0, err
	}
	var out struct {
		Auth struct {
			ClientToken   string `json:"client_token"`
			LeaseDuration int64  `json:"lease_duration"`
		} `json:"auth"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || out.Auth.ClientToken == "" {
		return "", 0, fmt.Errorf("login to %s: the reply holds no token", mount)
	}
	return out.Auth.ClientToken, time.Duration(out.Auth.LeaseDuration) * time.Second, nil
}

// current returns a usable token, logging in when needed. force asks for a
// fresh one because stale was refused.
func (s *session) current(ctx context.Context, force bool, stale string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.static {
		return s.token, nil
	}
	// Someone else already replaced the token that just failed.
	if force && s.token != "" && s.token != stale {
		return s.token, nil
	}
	if !force && s.token != "" && time.Now().Before(s.until) {
		return s.token, nil
	}
	tok, ttl, err := s.login(ctx)
	if err != nil {
		return "", err
	}
	s.token = tok
	if ttl <= 0 {
		ttl = time.Minute
	}
	s.until = time.Now().Add(ttl * 2 / 3)
	return tok, nil
}

// request sends one API call with the session's token. A 403 gets one fresh
// login and one retry; if that is refused too it is ErrForbidden.
func (c *Client) request(ctx context.Context, method, path string, body any) (int, []byte, error) {
	if c.session == nil {
		return 0, nil, errors.New("no Vault login configured")
	}
	tok, err := c.session.current(ctx, false, "")
	if err != nil {
		return 0, nil, err
	}
	status, resp, err := c.send(ctx, method, path, body, tok)
	if status == http.StatusForbidden && !c.session.static {
		fresh, lerr := c.session.current(ctx, true, tok)
		if lerr != nil {
			return 0, nil, lerr
		}
		status, resp, err = c.send(ctx, method, path, body, fresh)
	}
	if status == http.StatusForbidden {
		return status, resp, ErrForbidden
	}
	return status, resp, err
}

// send does the HTTP exchange: bounded reply, retries on 429 and 5xx with a
// backoff that gives up with the context. It never logs a body or a header.
func (c *Client) send(ctx context.Context, method, path string, body any, token string) (int, []byte, error) {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return 0, nil, fmt.Errorf("encode request: %w", err)
		}
	}
	backoff := 200 * time.Millisecond
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, c.addr+path, bytes.NewReader(payload))
		if err != nil {
			return 0, nil, fmt.Errorf("build request: %w", err)
		}
		if token != "" {
			req.Header.Set("X-Vault-Token", token)
		}
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := c.client.Do(req)
		var status int
		var reply []byte
		if err == nil {
			status = resp.StatusCode
			reply, err = io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
			_ = resp.Body.Close()
		}
		retryable := err != nil || status == http.StatusTooManyRequests || status >= 500
		if !retryable || attempt >= 2 {
			if err != nil {
				return 0, nil, fmt.Errorf("%s %s: %w", method, path, err)
			}
			return status, reply, nil
		}
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
		}
	}
}

// vaultMessage extracts Vault's error text, bounded, never the request body.
func vaultMessage(reply []byte) string {
	var e struct {
		Errors []string `json:"errors"`
	}
	msg := ""
	if json.Unmarshal(reply, &e) == nil {
		msg = strings.Join(e.Errors, "; ")
	}
	if len(msg) > 200 {
		msg = msg[:200]
	}
	return msg
}

func apiError(op string, status int, reply []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}
	msg := vaultMessage(reply)
	if status == http.StatusBadRequest && strings.Contains(msg, "check-and-set") {
		return fmt.Errorf("%s: %w", op, ErrCASMismatch)
	}
	return fmt.Errorf("%s: status %d: %s", op, status, msg)
}

// KVRead reads a KV v2 path, with its version and custom metadata.
func (c *Client) KVRead(ctx context.Context, mount, path string) (*KVSecret, error) {
	op := "read " + mount + "/" + path
	status, reply, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/%s/data/%s", mount, path), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if status != http.StatusOK && status != http.StatusNotFound {
		return nil, apiError(op, status, reply)
	}
	dec := json.NewDecoder(bytes.NewReader(reply))
	dec.UseNumber() // keep large integers exact when the data is written back
	var out struct {
		Data struct {
			Data     map[string]any `json:"data"`
			Metadata struct {
				Version        int64             `json:"version"`
				CustomMetadata map[string]string `json:"custom_metadata"`
			} `json:"metadata"`
		} `json:"data"`
	}
	if err := dec.Decode(&out); err != nil && status == http.StatusOK {
		return nil, fmt.Errorf("%s: decode reply: %w", op, err)
	}
	sec := &KVSecret{
		Exists:   status == http.StatusOK,
		Version:  out.Data.Metadata.Version,
		Data:     out.Data.Data,
		Metadata: out.Data.Metadata.CustomMetadata,
	}
	if !sec.Exists {
		// A deleted latest version answers 404 with its version number; a path
		// that never existed answers 404 with nothing.
		sec.Data = nil
		if sec.Version > 0 {
			// The custom metadata of a deleted path is still on its metadata
			// endpoint; read it so the ownership marker is not lost.
			md, err := c.kvMetadata(ctx, mount, path)
			if err != nil {
				return nil, err
			}
			sec.Metadata = md
		}
	}
	return sec, nil
}

// kvMetadata reads the custom metadata of a path; a missing path has none.
func (c *Client) kvMetadata(ctx context.Context, mount, path string) (map[string]string, error) {
	op := "read metadata " + mount + "/" + path
	status, reply, err := c.request(ctx, http.MethodGet, fmt.Sprintf("/v1/%s/metadata/%s", mount, path), nil)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", op, err)
	}
	if status == http.StatusNotFound {
		return nil, nil
	}
	if err := apiError(op, status, reply); err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			CustomMetadata map[string]string `json:"custom_metadata"`
		} `json:"data"`
	}
	if err := json.Unmarshal(reply, &out); err != nil {
		return nil, fmt.Errorf("%s: decode reply: %w", op, err)
	}
	return out.Data.CustomMetadata, nil
}

// KVWriteCAS writes a new version only if the path is still at version cas
// (0: the path must not exist). It returns the new version.
func (c *Client) KVWriteCAS(ctx context.Context, mount, path string, data map[string]any, cas int64) (int64, error) {
	op := "write " + mount + "/" + path
	body := map[string]any{"options": map[string]any{"cas": cas}, "data": data}
	status, reply, err := c.request(ctx, http.MethodPost, fmt.Sprintf("/v1/%s/data/%s", mount, path), body)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", op, err)
	}
	if err := apiError(op, status, reply); err != nil {
		return 0, err
	}
	var out struct {
		Data struct {
			Version int64 `json:"version"`
		} `json:"data"`
	}
	if err := json.Unmarshal(reply, &out); err != nil {
		return 0, fmt.Errorf("%s: decode reply: %w", op, err)
	}
	return out.Data.Version, nil
}

// KVSetCustomMetadata replaces the custom metadata of an existing path. It
// creates no KV version and leaves the path's other settings alone.
func (c *Client) KVSetCustomMetadata(ctx context.Context, mount, path string, md map[string]string) error {
	op := "write metadata " + mount + "/" + path
	status, reply, err := c.request(ctx, http.MethodPost, fmt.Sprintf("/v1/%s/metadata/%s", mount, path), map[string]any{"custom_metadata": md})
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	return apiError(op, status, reply)
}

// KVDestroy deletes a path with every version and its metadata. The policy
// grants it only when the chart allows deletion.
func (c *Client) KVDestroy(ctx context.Context, mount, path string) error {
	op := "delete " + mount + "/" + path
	status, reply, err := c.request(ctx, http.MethodDelete, fmt.Sprintf("/v1/%s/metadata/%s", mount, path), nil)
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}
	if status == http.StatusNotFound {
		return nil
	}
	return apiError(op, status, reply)
}
