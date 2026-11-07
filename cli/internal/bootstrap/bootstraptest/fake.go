// Package bootstraptest is a bootstrap.Cluster for tests: it records what it
// is asked and lets the test decide what the cluster holds.
package bootstraptest

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/this-is-tobi/ohmlab/cli/internal/bootstrap"
)

// Cluster holds CRDs by name and the password Argo CD generated.
type Cluster struct {
	// CRDs are the CRDs present; Apply adds the ones it gets.
	CRDs    map[string]bool
	Applied []*unstructured.Unstructured
	Waited  []string
	// Password is the generated admin password; empty, there is none.
	Password string
	// Namespaces are the ones InitialAdminPassword was asked about.
	Namespaces []string
	// Err fails a method, keyed by its name.
	Err map[string]error
}

var _ bootstrap.Cluster = (*Cluster)(nil)

func (c *Cluster) HasCRD(_ context.Context, name string) (bool, error) {
	if err := c.Err["HasCRD"]; err != nil {
		return false, err
	}
	return c.CRDs[name], nil
}

func (c *Cluster) Apply(_ context.Context, objs []*unstructured.Unstructured) error {
	if err := c.Err["Apply"]; err != nil {
		return err
	}
	c.Applied = append(c.Applied, objs...)
	for _, o := range objs {
		if o.GetKind() == "CustomResourceDefinition" {
			if c.CRDs == nil {
				c.CRDs = map[string]bool{}
			}
			c.CRDs[o.GetName()] = true
		}
	}
	return nil
}

func (c *Cluster) WaitEstablished(_ context.Context, names []string, _ time.Duration) error {
	c.Waited = append(c.Waited, names...)
	return c.Err["WaitEstablished"]
}

func (c *Cluster) InitialAdminPassword(_ context.Context, namespace string) (string, error) {
	c.Namespaces = append(c.Namespaces, namespace)
	if err := c.Err["InitialAdminPassword"]; err != nil {
		return "", err
	}
	if c.Password == "" {
		return "", fmt.Errorf("%w in %s", bootstrap.ErrNoInitialPassword, namespace)
	}
	return c.Password, nil
}
