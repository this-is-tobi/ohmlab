package instance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// instances makes an instances directory: a folder per name, each with an
// instance.yaml unless the name ends with a slash.
func instances(t *testing.T, names ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, n := range names {
		dir := filepath.Join(root, strings.TrimSuffix(n, "/"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(n, "/") {
			if err := os.WriteFile(filepath.Join(dir, File), []byte("instance: {}\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return root
}

func TestPickTheOnlyInstance(t *testing.T) {
	dir := instances(t, "_example", "lab")
	got, err := Pick(dir, "")
	if err != nil || got != (Instance{Name: "lab", Dir: filepath.Join(dir, "lab")}) {
		t.Errorf("got %+v, %v; want lab", got, err)
	}
}

func TestPickNamed(t *testing.T) {
	dir := instances(t, "lab", "prod", "_example")
	if got, err := Pick(dir, "lab"); err != nil || got.Name != "lab" || got.Dir != filepath.Join(dir, "lab") {
		t.Errorf("got %+v, %v; want lab", got, err)
	}
	_, err := Pick(dir, "nope")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "nope")) || !strings.Contains(err.Error(), File) {
		t.Errorf("got %v, want an error naming the folder and %s", err, File)
	}
	for _, bad := range []string{"_example", "../lab", "lab/x"} {
		if _, err := Pick(dir, bad); err == nil {
			t.Errorf("Pick(%q) accepted it", bad)
		}
	}
}

func TestPickNeedsANameAmongSeveral(t *testing.T) {
	_, err := Pick(instances(t, "prod", "lab"), "")
	for _, want := range []string{"lab, prod", "--instance", "instance in ohmlab.yaml"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("got %v, want an error with %q", err, want)
		}
	}
}

func TestPickNone(t *testing.T) {
	_, err := Pick(instances(t, "_example"), "")
	if err == nil || !strings.Contains(err.Error(), "starting with _ are examples") {
		t.Errorf("got %v, want an error saying _ folders are examples", err)
	}
}

func TestPickIgnoresFoldersWithoutInstanceFile(t *testing.T) {
	if got, err := Pick(instances(t, "lab", "notes/"), ""); err != nil || got.Name != "lab" {
		t.Errorf("got %+v, %v; want lab", got, err)
	}
}
