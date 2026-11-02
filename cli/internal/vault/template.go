package vault

import (
	"fmt"

	"github.com/this-is-tobi/ohmlab/utils/internal/secretgen"
)

// TemplateEngine processes placeholder strings in secret data.
//
// Supported placeholders:
//
//	<random:N>   — cryptographically random alphanumeric string of length N
//	<hex:N>      — cryptographically random hex string of length N (N must be even)
//	<uuid>       — random UUID v4
//	<age:secret> — age X25519 secret key (generated once per Process call)
//	<age:public> — age X25519 public key (paired with the secret key)
//	<ref:path#key.path> — reference a value from another Vault secret
//	<bcrypt:key.path>   — bcrypt hash of a field in the same secret (resolved after processing)
//
// The generation itself lives in secretgen, which the controller shares.
type TemplateEngine struct {
	vaultClient *Client // for resolving <ref:...> placeholders
	renderer    secretgen.Renderer
}

// NewTemplateEngine creates a template engine with an optional Vault client
// for resolving cross-references.
func NewTemplateEngine(vaultClient *Client) *TemplateEngine {
	e := &TemplateEngine{vaultClient: vaultClient}
	if vaultClient != nil {
		e.renderer.Ref = e.resolveRef
	}
	return e
}

// Process walks a data map and replaces all placeholder strings with generated values.
// Each call resets the age keypair so that <age:secret> and <age:public> are paired.
func (e *TemplateEngine) Process(data map[string]any) (map[string]any, error) {
	e.renderer.Age = nil // reset per-secret age keypair
	result, err := e.processMap(data)
	if err != nil {
		return nil, err
	}
	// Second pass: resolve <bcrypt:key.path> after all other placeholders.
	return e.resolveBcryptFields(result, result)
}

func (e *TemplateEngine) processMap(data map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(data))
	for k, v := range data {
		processed, err := e.processValue(v)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		result[k] = processed
	}
	return result, nil
}

func (e *TemplateEngine) processValue(v any) (any, error) {
	switch val := v.(type) {
	case string:
		return e.renderer.Render(val)
	case map[string]any:
		return e.processMap(val)
	case []any:
		result := make([]any, len(val))
		for i, item := range val {
			processed, err := e.processValue(item)
			if err != nil {
				return nil, fmt.Errorf("index %d: %w", i, err)
			}
			result[i] = processed
		}
		return result, nil
	default:
		return v, nil
	}
}

// resolveRef reads a <ref:vaultPath#dotPath> source from Vault.
func (e *TemplateEngine) resolveRef(vaultPath, keyPath string) (string, error) {
	// Convention: first path segment is the mount, rest is the path.
	mount, secretPath, ok := splitFirst(vaultPath)
	if !ok {
		return "", fmt.Errorf("invalid ref vault path %q", vaultPath)
	}
	data, err := e.vaultClient.KVGet(mount, secretPath)
	if err != nil {
		return "", fmt.Errorf("failed to read ref from Vault: %w", err)
	}
	if data == nil {
		return "", fmt.Errorf("ref secret not found in Vault (path %s): %w", vaultPath, secretgen.ErrRefNotFound)
	}
	val := navigateJSON(data, keyPath)
	if val == "" {
		return "", fmt.Errorf("ref key path %q not found or empty: %w", keyPath, secretgen.ErrRefNotFound)
	}
	return val, nil
}

func splitFirst(p string) (string, string, bool) {
	for i := 0; i < len(p); i++ {
		if p[i] == '/' {
			return p[:i], p[i+1:], i > 0 && i+1 < len(p)
		}
	}
	return "", "", false
}

// navigateJSON traverses a nested map using a dot-separated key path.
func navigateJSON(data map[string]any, dotPath string) string {
	return secretgen.NavigateJSON(data, dotPath)
}

// resolveBcryptFields walks a processed data map and replaces any remaining
// <bcrypt:key.path> placeholders with the bcrypt hash of the referenced field
// value from the root data map.
func (e *TemplateEngine) resolveBcryptFields(data, root map[string]any) (map[string]any, error) {
	result := make(map[string]any, len(data))
	for k, v := range data {
		resolved, err := e.resolveBcryptValue(v, root)
		if err != nil {
			return nil, fmt.Errorf("key %q: %w", k, err)
		}
		result[k] = resolved
	}
	return result, nil
}

func (e *TemplateEngine) resolveBcryptValue(v any, root map[string]any) (any, error) {
	switch val := v.(type) {
	case string:
		keyPath, ok := secretgen.BcryptTarget(val)
		if !ok {
			return val, nil
		}
		plaintext := navigateJSON(root, keyPath)
		if plaintext == "" {
			return "", fmt.Errorf("bcrypt: field %q not found or empty in secret data", keyPath)
		}
		return secretgen.Bcrypt(plaintext)
	case map[string]any:
		return e.resolveBcryptFields(val, root)
	case []any:
		out := make([]any, len(val))
		for i, item := range val {
			resolved, err := e.resolveBcryptValue(item, root)
			if err != nil {
				return nil, fmt.Errorf("index %d: %w", i, err)
			}
			out[i] = resolved
		}
		return out, nil
	default:
		return v, nil
	}
}
