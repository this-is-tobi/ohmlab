package vault

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// SecretEntry represents a single secret to initialize in Vault.
type SecretEntry struct {
	Path string         `json:"path"`
	Data map[string]any `json:"data"`
}

// InitConfig holds the full configuration for vault secret initialization.
type InitConfig struct {
	Entries []SecretEntry `json:"entries"`
}

// InitOptions controls the behavior of the init process.
type InitOptions struct {
	DryRun      bool // Print what would be written without writing
	ForceRotate bool // Overwrite existing values instead of preserving them
	ConfigFile  string
	VaultMount  string // KV mount extracted from path (first segment)
}

// InitResult holds the result of processing a single secret entry.
type InitResult struct {
	Path   string
	Action string // "created", "updated", "unchanged", "skipped" (dry-run)
	Added  int    // number of new keys added
	Error  error
}

// RunInit processes all secret entries: generate values, merge, write.
func RunInit(client *Client, entries []SecretEntry, opts InitOptions) []InitResult {
	engine := NewTemplateEngine(client)
	results := make([]InitResult, 0, len(entries))

	for _, entry := range entries {
		result := processEntry(client, engine, entry, opts)
		results = append(results, result)

		if result.Error != nil {
			slog.Error("failed to process secret",
				"path", entry.Path,
				"error", result.Error,
			)
		} else {
			slog.Info("processed secret",
				"path", entry.Path,
				"action", result.Action,
				"added", result.Added,
			)
		}
	}

	return results
}

func processEntry(client *Client, engine *TemplateEngine, entry SecretEntry, opts InitOptions) InitResult {
	result := InitResult{Path: entry.Path}

	// Parse mount and path from the entry path (e.g., "homelab/platforms/production/gitea")
	mount, secretPath, err := splitMountPath(entry.Path)
	if err != nil {
		result.Error = err
		return result
	}

	// Process template placeholders
	newData, err := engine.Process(entry.Data)
	if err != nil {
		result.Error = fmt.Errorf("process templates: %w", err)
		return result
	}

	if opts.DryRun {
		result.Action = "skipped"
		slog.Info("dry-run: would process secret", "path", entry.Path)
		return result
	}

	// Read existing secret from Vault
	existing, err := client.KVGet(mount, secretPath)
	if err != nil {
		result.Error = fmt.Errorf("read existing: %w", err)
		return result
	}

	if existing == nil {
		// Secret doesn't exist — create it
		if err := client.KVPut(mount, secretPath, newData); err != nil {
			result.Error = fmt.Errorf("create secret: %w", err)
			return result
		}
		result.Action = "created"
		result.Added = countLeaves(newData)
		return result
	}

	if opts.ForceRotate {
		// Force mode — overwrite with new values
		if err := client.KVPut(mount, secretPath, newData); err != nil {
			result.Error = fmt.Errorf("force-write secret: %w", err)
			return result
		}
		result.Action = "rotated"
		result.Added = countLeaves(newData)
		return result
	}

	// Deep merge: existing values preserved, only missing keys added
	merged := DeepMerge(existing, newData)

	if Equal(existing, merged) {
		result.Action = "unchanged"
		return result
	}

	// Write the merged data
	if err := client.KVPut(mount, secretPath, merged); err != nil {
		result.Error = fmt.Errorf("update secret: %w", err)
		return result
	}
	result.Action = "updated"
	result.Added = countLeaves(merged) - countLeaves(existing)
	return result
}

// LoadConfig reads a JSON config file containing secret entries.
// Accepts both a bare array and an object with an "entries" key.
func LoadConfig(path string) ([]SecretEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	// Trim whitespace
	data = []byte(strings.TrimSpace(string(data)))

	// Try as array first (backward compatible with bash script format)
	var entries []SecretEntry
	if err := json.Unmarshal(data, &entries); err == nil {
		return entries, nil
	}

	// Try as object with entries key
	var cfg InitConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	return cfg.Entries, nil
}

// splitMountPath splits "mount/rest/of/path" into ("mount", "rest/of/path").
func splitMountPath(fullPath string) (string, string, error) {
	parts := strings.SplitN(fullPath, "/", 2)
	if len(parts) < 2 {
		return "", "", fmt.Errorf("invalid vault path %q: expected mount/path format", fullPath)
	}
	return parts[0], parts[1], nil
}

// countLeaves counts the total number of leaf values in a nested map.
func countLeaves(m map[string]any) int {
	count := 0
	for _, v := range m {
		if nested, ok := v.(map[string]any); ok {
			count += countLeaves(nested)
		} else {
			count++
		}
	}
	return count
}
