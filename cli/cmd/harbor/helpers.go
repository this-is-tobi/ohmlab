package harbor

import "os"

// envOrDefault returns the value of an environment variable or a default value if not set.
func envOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
