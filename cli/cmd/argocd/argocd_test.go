package argocd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	bs "github.com/this-is-tobi/ohmlab/cli/internal/bootstrap"
	"github.com/this-is-tobi/ohmlab/cli/internal/bootstrap/bootstraptest"
)

// workspace makes a directory holding an ohmlab.yaml with content.
func workspace(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ohmlab.yaml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KUBE_CONTEXT_NAME", "")
	return dir
}

// connector hands out one fake cluster and records the contexts asked for.
type connector struct {
	cluster *bootstraptest.Cluster
	asked   []string
}

func (c *connector) connect(_, context string) (bs.Cluster, string, error) {
	c.asked = append(c.asked, context)
	return c.cluster, "https://kind:6443", nil
}

func defaults() passwordOptions { return passwordOptions{namespace: bs.DefaultNamespace} }

func TestPasswordPrintsTheBarePassword(t *testing.T) {
	var out bytes.Buffer
	c := &connector{cluster: &bootstraptest.Cluster{Password: "pw-123"}}
	if err := runPassword(context.Background(), c.connect, &out, workspace(t, ""), os.Getenv, defaults()); err != nil {
		t.Fatal(err)
	}
	if out.String() != "pw-123\n" {
		t.Errorf("printed %q, want the password and a newline alone", out.String())
	}
}

func TestPasswordWithoutTheSecret(t *testing.T) {
	var out bytes.Buffer
	c := &connector{cluster: &bootstraptest.Cluster{}}
	err := runPassword(context.Background(), c.connect, &out, workspace(t, ""), os.Getenv, defaults())
	if !errors.Is(err, bs.ErrNoInitialPassword) {
		t.Errorf("got %v, want ErrNoInitialPassword", err)
	}
	for _, want := range []string{"deleted once the password is changed", "bootstrap set the password"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("got %v, want an error with %q", err, want)
		}
	}
	if out.Len() != 0 {
		t.Errorf("printed %q", out.String())
	}
}

func TestPasswordUsesTheWorkspaceContext(t *testing.T) {
	ws := workspace(t, "contextName: lab\n")
	for flag, want := range map[string]string{"": "lab", "cli": "cli"} {
		var out bytes.Buffer
		c := &connector{cluster: &bootstraptest.Cluster{Password: "pw-123"}}
		o := defaults()
		o.workspace.ContextName = flag
		if err := runPassword(context.Background(), c.connect, &out, ws, os.Getenv, o); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(c.asked, []string{want}) {
			t.Errorf("--context %q: connected to %v, want %s", flag, c.asked, want)
		}
	}
}

func TestPasswordNamespace(t *testing.T) {
	var out bytes.Buffer
	c := &connector{cluster: &bootstraptest.Cluster{Password: "pw-123"}}
	o := defaults()
	o.namespace = "other"
	if err := runPassword(context.Background(), c.connect, &out, workspace(t, ""), os.Getenv, o); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(c.cluster.Namespaces, []string{"other"}) {
		t.Errorf("asked %v, want other", c.cluster.Namespaces)
	}
}
