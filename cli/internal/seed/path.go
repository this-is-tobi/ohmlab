package seed

import (
	"errors"
	"fmt"
	"strings"
)

// SplitPath splits "<mount>/<path>" and rejects anything that is not one.
func SplitPath(full string) (mount, path string, err error) {
	mount, path, ok := strings.Cut(full, "/")
	if !ok || mount == "" || path == "" || strings.HasSuffix(path, "/") ||
		strings.Contains(full, "..") || strings.Contains(full, "//") {
		return "", "", fmt.Errorf("invalid path %q: want <mount>/<path>", full)
	}
	return mount, path, nil
}

// ErrInvalidPath marks a path that is not "<mount>/<path>".
var ErrInvalidPath = errors.New("invalid path")
