package kubeconfig

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ServerAddress is where the workstation reaches the cluster's API server:
// the first host of the gateway group (HAProxy fronts the API server), else
// of the masters group, found at any depth of children in
// <inventory>/hosts.yml, by its ansible_host or, as Ansible does, its name.
// Nodes keep the file's order, so "first" is the one written first.
func ServerAddress(inventory string) (string, error) {
	path := filepath.Join(inventory, "hosts.yml")
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if len(doc.Content) > 0 {
		for _, group := range []string{"gateway", "masters"} {
			name, vars := firstHost(findGroup(doc.Content[0], group))
			if name == "" {
				continue
			}
			if h := value(vars, "ansible_host"); h != nil && h.Kind == yaml.ScalarNode && h.Value != "" {
				return h.Value, nil
			}
			return name, nil
		}
	}
	return "", fmt.Errorf("%s: no host in a gateway or masters group", path)
}

// findGroup returns the group called name in a mapping of groups, searching
// their children depth first.
func findGroup(groups *yaml.Node, name string) *yaml.Node {
	if groups == nil || groups.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(groups.Content); i += 2 {
		if groups.Content[i].Value == name {
			return groups.Content[i+1]
		}
		if g := findGroup(value(groups.Content[i+1], "children"), name); g != nil {
			return g
		}
	}
	return nil
}

// firstHost returns the first host of a group, its own or else its
// children's, with its variables.
func firstHost(group *yaml.Node) (string, *yaml.Node) {
	if hosts := value(group, "hosts"); hosts != nil && hosts.Kind == yaml.MappingNode && len(hosts.Content) >= 2 {
		return hosts.Content[0].Value, hosts.Content[1]
	}
	if children := value(group, "children"); children != nil && children.Kind == yaml.MappingNode {
		for i := 1; i < len(children.Content); i += 2 {
			if name, vars := firstHost(children.Content[i]); name != "" {
				return name, vars
			}
		}
	}
	return "", nil
}

// value is the value of key in a mapping node, or nil.
func value(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}
