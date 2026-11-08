package provision

import (
	"fmt"
	"path/filepath"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// Context is a context of the user's kubeconfig, made the only one of a
// kubeconfig: the k3s-users play acts on the current context of the file it
// gets, which must not be whichever one the user has current.
type Context struct {
	Name, Server string
	Found        bool
	config       *clientcmdapi.Config
}

// LoadContext reads the user's kubeconfig as kubectl does (kubeconfig, else
// $KUBECONFIG, else ~/.kube/config) and keeps the named context alone, as the
// current one. A missing context is not an error: Found is false.
func LoadContext(kubeconfig, name string) (Context, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfig
	// A missing context is reported by the command, with what to do.
	rules.WarnIfAllMissing = false
	cfg, err := rules.Load()
	if err != nil {
		return Context{}, fmt.Errorf("read kubeconfig: %w", err)
	}
	c := Context{Name: name}
	x, ok := cfg.Contexts[name]
	if !ok {
		return c, nil
	}
	cfg.CurrentContext = name
	if err := clientcmdapi.MinifyConfig(cfg); err != nil {
		return Context{}, fmt.Errorf("context %s: %w", name, err)
	}
	// MinifyConfig keeps a context that names no cluster, as kubectl
	// config set-context can leave one.
	cluster := cfg.Clusters[x.Cluster]
	if cluster == nil {
		return Context{}, fmt.Errorf("context %s names no cluster: fix it in the kubeconfig, or fetch it again with ohmlab kubeconfig", name)
	}
	c.Found, c.Server, c.config = true, cluster.Server, cfg
	return c, nil
}

// Write writes the kubeconfig to dir, readable by its owner only, and
// returns its path. Without the context it holds none, so whatever reads it
// reaches no cluster.
func (c Context) Write(dir string) (string, error) {
	cfg := c.config
	if cfg == nil {
		cfg = clientcmdapi.NewConfig()
	}
	path := filepath.Join(dir, "kubeconfig")
	if err := clientcmd.WriteToFile(*cfg, path); err != nil {
		return "", err
	}
	return path, nil
}
