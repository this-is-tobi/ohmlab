// Package instance finds the instance a command acts on among the folders of
// an instances directory (argo-cd/instances).
package instance

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// File is what makes a folder an instance.
const File = "instance.yaml"

// Instance is one instance folder.
type Instance struct {
	Name, Dir string
}

// Pick returns the instance called name, or the only one when name is
// empty. A folder starting with _ is an example, as for the root manager,
// and one without instance.yaml is not an instance.
func Pick(instancesDir, name string) (Instance, error) {
	if name != "" {
		if name != filepath.Base(name) || name == "." || name == ".." {
			return Instance{}, fmt.Errorf("instance %q is not a folder name", name)
		}
		if strings.HasPrefix(name, "_") {
			return Instance{}, fmt.Errorf("%s is an example: folders starting with _ are not instances", name)
		}
		dir := filepath.Join(instancesDir, name)
		if !isFile(filepath.Join(dir, File)) {
			return Instance{}, fmt.Errorf("no instance %s: %s has no %s", name, dir, File)
		}
		return Instance{Name: name, Dir: dir}, nil
	}

	entries, err := os.ReadDir(instancesDir)
	if err != nil {
		return Instance{}, fmt.Errorf("read the instances: %w", err)
	}
	var found []Instance
	for _, e := range entries {
		dir := filepath.Join(instancesDir, e.Name())
		if e.IsDir() && !strings.HasPrefix(e.Name(), "_") && isFile(filepath.Join(dir, File)) {
			found = append(found, Instance{Name: e.Name(), Dir: dir})
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return Instance{}, fmt.Errorf("no instance in %s (folders starting with _ are examples)", instancesDir)
	}
	names := make([]string, len(found))
	for i, in := range found {
		names[i] = in.Name
	}
	return Instance{}, fmt.Errorf("%d instances in %s (%s): choose one with --instance, or set instance in ohmlab.yaml", len(found), instancesDir, strings.Join(names, ", "))
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
