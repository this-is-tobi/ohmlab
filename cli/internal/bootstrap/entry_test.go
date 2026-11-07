package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const catalogs = "catalogs:\n  ohmlab: ghcr.io/example/charts\n"

// instanceDir makes an instance folder whose core.yaml lists apps (YAML lines
// under apps:) and whose instance.yaml ends with extra.
func instanceDir(t *testing.T, apps, extra string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "core.yaml"), "apps:\n"+apps)
	write(t, filepath.Join(dir, "instance.yaml"), "instance:\n  name: t\n"+extra)
	return dir
}

// product makes a checkout holding a Chart.yaml in each of the given
// directories ("." is the checkout itself).
func product(t *testing.T, charts ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, c := range charts {
		write(t, filepath.Join(dir, c, "Chart.yaml"), "apiVersion: v2\nname: ohmlab\nversion: 0.0.0\n")
	}
	return dir
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolveChartDecisionTable(t *testing.T) {
	prod := product(t, "charts/ohmlab")
	chartDir := product(t, ".")
	pinned := "- app: ohmlab\n  catalog: ohmlab\n  targetRevision: "
	const exact = "exact chart version in targetRevision"
	for _, tc := range []struct {
		name, apps, extra, dir string
		want                   Chart
		wantErr                []string
	}{
		{name: "no catalog uses the local chart", apps: "- app: ohmlab\n  chartPath: charts/ohmlab\n",
			want: Chart{Ref: filepath.Join(prod, "charts", "ohmlab")}},
		{name: "a chart directory wins, even over a catalog", apps: pinned + "0.1.3\n", extra: catalogs, dir: chartDir,
			want: Chart{Ref: chartDir}},
		{name: "no ohmlab entry uses the local chart", apps: "- app: other\n  enabled: \"true\"\n",
			want: Chart{Ref: filepath.Join(prod, "charts", "ohmlab")}},
		{name: "a catalog entry gives the registry chart and its version", apps: pinned + "0.1.3\n", extra: catalogs,
			want: Chart{Ref: "oci://ghcr.io/example/charts/ohmlab", Version: "0.1.3"}},
		{name: "the chart name and a prerelease version are kept",
			apps: "- app: ohmlab\n  chart: ohmlab-bootstrap\n  catalog: ohmlab\n  targetRevision: 1.2.3-rc.1\n", extra: catalogs,
			want: Chart{Ref: "oci://ghcr.io/example/charts/ohmlab-bootstrap", Version: "1.2.3-rc.1"}},
		{name: "an undeclared catalog is refused", apps: "- app: ohmlab\n  catalog: nope\n  targetRevision: 0.1.3\n", extra: catalogs,
			wantErr: []string{"not declared under catalogs", "instance.yaml"}},
		{name: "a catalog without any catalogs section is refused", apps: pinned + "0.1.3\n",
			wantErr: []string{"not declared under catalogs"}},
		{name: "a catalog entry without a version is refused", apps: "- app: ohmlab\n  catalog: ohmlab\n", extra: catalogs,
			wantErr: []string{exact, "core.yaml"}},
		{name: "a range is refused", apps: pinned + "^0.1.0\n", extra: catalogs, wantErr: []string{exact}},
		{name: "a tag is refused", apps: pinned + "latest\n", extra: catalogs, wantErr: []string{exact}},
		{name: "a bundle catalog cannot give the bootstrap chart", apps: pinned + "0.1.3\n",
			extra:   "catalogs:\n  ohmlab:\n    repoURL: oci://ghcr.io/example/catalog\n    version: 0.2.0\n",
			wantErr: []string{"registry path"}},
		{name: "a chart directory without Chart.yaml is refused", apps: "- app: ohmlab\n  chartPath: charts/missing\n",
			wantErr: []string{"--chart", "--product"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := instanceDir(t, tc.apps, tc.extra)
			e, err := ReadEntry(dir)
			if err != nil {
				t.Fatal(err)
			}
			got, err := ResolveChart(e, dir, prod, tc.dir)
			if len(tc.wantErr) > 0 {
				for _, w := range tc.wantErr {
					if err == nil || !strings.Contains(err.Error(), w) {
						t.Errorf("got %+v, %v; want an error with %q", got, err, w)
					}
				}
				return
			}
			if err != nil || got != tc.want {
				t.Errorf("got %+v, %v; want %+v", got, err, tc.want)
			}
		})
	}
}

func TestChartPathComesFromTheEntry(t *testing.T) {
	prod := product(t, "charts/other")
	dir := instanceDir(t, "- app: ohmlab\n  chartPath: charts/other\n", "")
	e, err := ReadEntry(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := ResolveChart(e, dir, prod, ""); err != nil || got != (Chart{Ref: filepath.Join(prod, "charts", "other")}) {
		t.Errorf("got %+v, %v; want the entry's chartPath in the product", got, err)
	}
}

func TestReleaseDefaults(t *testing.T) {
	if got := (Entry{}).Release(); got != (Release{Name: "ohmlab", Namespace: "argocd-system"}) {
		t.Errorf("got %+v, want ohmlab in argocd-system", got)
	}
	e, err := ReadEntry(instanceDir(t, "- app: ohmlab\n  releaseName: boot\n  namespace: gitops\n", ""))
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Release(); !e.Found || got != (Release{Name: "boot", Namespace: "gitops"}) {
		t.Errorf("got %+v (found %v), want the entry's boot in gitops", got, e.Found)
	}
	if e, _ := ReadEntry(instanceDir(t, "- app: other\n", "")); e.Found {
		t.Error("an instance without an ohmlab app has an entry")
	}
}

func TestReadEntryNeedsCoreYAML(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadEntry(dir)
	if want := filepath.Join(dir, "core.yaml"); err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("got %v, want an error naming %s", err, want)
	}
}
