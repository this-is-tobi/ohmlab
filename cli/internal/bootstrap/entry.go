// Package bootstrap installs an instance's bootstrap chart on its cluster:
// the Gateway API CRDs when they are missing, Argo CD alone on an empty
// cluster, then the whole chart. Helm does the installing; this package
// decides what it installs, and where.
package bootstrap

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

// DefaultNamespace is where the bootstrap release goes when the instance's
// ohmlab entry names no namespace.
const DefaultNamespace = "argocd-system"

const defaultRelease = "ohmlab"

// Entry is the ohmlab app of an instance's core.yaml: the release its
// self-managed Application adopts after the bootstrap.
type Entry struct {
	// Found is false when core.yaml lists no ohmlab app.
	Found bool

	Chart, ChartPath, Catalog, TargetRevision, Namespace, ReleaseName string
}

// ReadEntry reads the ohmlab app of <instanceDir>/core.yaml.
func ReadEntry(instanceDir string) (Entry, error) {
	path := filepath.Join(instanceDir, "core.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	var core struct {
		Apps []struct {
			App            string `yaml:"app"`
			Chart          string `yaml:"chart"`
			ChartPath      string `yaml:"chartPath"`
			Catalog        string `yaml:"catalog"`
			TargetRevision string `yaml:"targetRevision"`
			Namespace      string `yaml:"namespace"`
			ReleaseName    string `yaml:"releaseName"`
		} `yaml:"apps"`
	}
	if err := yaml.Unmarshal(data, &core); err != nil {
		return Entry{}, fmt.Errorf("%s: %w", path, err)
	}
	for _, a := range core.Apps {
		if a.App == "ohmlab" {
			return Entry{Found: true, Chart: a.Chart, ChartPath: a.ChartPath, Catalog: a.Catalog,
				TargetRevision: a.TargetRevision, Namespace: a.Namespace, ReleaseName: a.ReleaseName}, nil
		}
	}
	return Entry{}, nil
}

// Release is the Helm release a bootstrap installs.
type Release struct{ Name, Namespace string }

// Release is the entry's release, else ohmlab in DefaultNamespace.
func (e Entry) Release() Release {
	r := Release{Name: e.ReleaseName, Namespace: e.Namespace}
	if r.Name == "" {
		r.Name = defaultRelease
	}
	if r.Namespace == "" {
		r.Namespace = DefaultNamespace
	}
	return r
}

// Chart is what Helm installs: a directory, or a published chart at an exact
// version.
type Chart struct{ Ref, Version string }

// Local reports whether the chart is a directory, whose dependencies Helm
// fetches first.
func (c Chart) Local() bool { return c.Version == "" }

// An exact version only: a range or a tag would not be what the self-managed
// Application runs, and a registry tag can be re-pushed.
var exactVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$`)

// ResolveChart returns the chart the self-managed Application runs: dir
// (--chart) when set; else, when the entry has a catalog, the published chart
// at the version pinned in targetRevision; else the entry's chartPath
// (charts/ohmlab by default) in the product checkout.
func ResolveChart(e Entry, instanceDir, product, dir string) (Chart, error) {
	if dir != "" {
		return localChart(dir)
	}
	if e.Catalog == "" {
		path := e.ChartPath
		if path == "" {
			path = filepath.Join("charts", "ohmlab")
		}
		return localChart(filepath.Join(product, path))
	}
	registry, err := registryOf(filepath.Join(instanceDir, "instance.yaml"), e.Catalog)
	if err != nil {
		return Chart{}, err
	}
	if !exactVersion.MatchString(e.TargetRevision) {
		return Chart{}, fmt.Errorf("the ohmlab entry of %s needs an exact chart version in targetRevision (got %q)",
			filepath.Join(instanceDir, "core.yaml"), e.TargetRevision)
	}
	chart := e.Chart
	if chart == "" {
		chart = "ohmlab"
	}
	return Chart{Ref: "oci://" + registry + "/" + chart, Version: e.TargetRevision}, nil
}

// registryOf returns the registry path a catalog of instance.yaml names.
func registryOf(path, catalog string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var inst struct {
		Catalogs map[string]yaml.Node `yaml:"catalogs"`
	}
	if err := yaml.Unmarshal(data, &inst); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	node, ok := inst.Catalogs[catalog]
	if !ok || node.Tag == "!!null" || (node.Kind == yaml.ScalarNode && node.Value == "") {
		return "", fmt.Errorf("catalog %q is not declared under catalogs in %s", catalog, path)
	}
	// A bundle ({repoURL, version}) is a tree for Argo CD, not a chart Helm
	// can install.
	if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
		return "", fmt.Errorf("catalog %q is a bundle; the ohmlab chart needs a registry path (a string) under catalogs in %s", catalog, path)
	}
	return node.Value, nil
}

func localChart(dir string) (Chart, error) {
	if _, err := os.Stat(filepath.Join(dir, "Chart.yaml")); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Chart{}, fmt.Errorf("no Chart.yaml in %s: pass --chart with the chart's directory, or --product with a checkout of ohmlab", dir)
		}
		return Chart{}, err
	}
	return Chart{Ref: dir}, nil
}
