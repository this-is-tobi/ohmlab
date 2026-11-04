// Package kubeclient loads the kubeconfig the way kubectl does, for the
// commands run from a workstation: exec plugins (Teleport, cloud CLIs) work.
// What runs in the cluster uses internal/k8s, which runs no plugin.
package kubeclient

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Load returns the client configuration of a context and its name. An empty
// kubeconfig path follows $KUBECONFIG, then ~/.kube/config; an empty context
// is the current one.
func Load(kubeconfig, context string) (*rest.Config, string, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfig
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, &clientcmd.ConfigOverrides{CurrentContext: context})
	raw, err := loader.RawConfig()
	if err != nil {
		return nil, "", fmt.Errorf("read kubeconfig: %w", err)
	}
	name := context
	if name == "" {
		name = raw.CurrentContext
	}
	cfg, err := loader.ClientConfig()
	if err != nil {
		return nil, "", fmt.Errorf("context %q: %w", name, err)
	}
	return cfg, name, nil
}
