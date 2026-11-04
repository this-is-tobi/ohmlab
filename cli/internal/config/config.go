// Package config reads the workspace file, ohmlab.yaml at the root of an
// instance repository.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// DefaultPath is where the commands look for the workspace file.
const DefaultPath = "ohmlab.yaml"

// File is the workspace file.
type File struct {
	Audit Audit `yaml:"audit"`
}

// Audit configures the audit commands.
type Audit struct {
	TLS AuditTLS `yaml:"tls"`
}

// AuditTLS configures `ohmlab audit tls`.
type AuditTLS struct {
	Exceptions []TLSException `yaml:"exceptions"`
}

// TLSException lets the ports of a Service stay plaintext, for a reason.
type TLSException struct {
	Namespace string  `yaml:"namespace" json:"namespace"`
	Service   string  `yaml:"service" json:"service"`
	Ports     []int32 `yaml:"ports,omitempty" json:"ports,omitempty"` // empty: every port
	Reason    string  `yaml:"reason" json:"reason"`
}

// Load reads and checks the workspace file at path. Unknown keys are errors,
// so a typo cannot silently drop an exception. A missing file is an error
// that wraps fs.ErrNotExist.
func Load(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	for i, e := range f.Audit.TLS.Exceptions {
		if e.Namespace == "" || e.Service == "" || e.Reason == "" {
			return nil, fmt.Errorf("%s: audit.tls.exceptions[%d] (%s/%s) needs a namespace, a service and a reason", path, i, e.Namespace, e.Service)
		}
	}
	return &f, nil
}
