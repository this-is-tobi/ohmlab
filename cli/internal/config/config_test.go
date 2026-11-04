package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), DefaultPath)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadReadsExceptions(t *testing.T) {
	f, err := Load(write(t, `audit:
  tls:
    exceptions:
    - namespace: homepage
      service: homepage
      ports: [3000]
      reason: serves HTTP only
`))
	if err != nil {
		t.Fatal(err)
	}
	want := []TLSException{{Namespace: "homepage", Service: "homepage", Ports: []int32{3000}, Reason: "serves HTTP only"}}
	if !reflect.DeepEqual(f.Audit.TLS.Exceptions, want) {
		t.Errorf("exceptions = %+v, want %+v", f.Audit.TLS.Exceptions, want)
	}
}

func TestLoadEmptyFile(t *testing.T) {
	f, err := Load(write(t, "# nothing yet\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Audit.TLS.Exceptions) != 0 {
		t.Errorf("exceptions = %+v, want none", f.Audit.TLS.Exceptions)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	_, err := Load(write(t, "audit:\n  tls:\n    exeptions: []\n"))
	if err == nil || !strings.Contains(err.Error(), "exeptions") {
		t.Fatalf("err = %v, want one naming exeptions", err)
	}
}

func TestLoadRequiresNamespaceServiceReason(t *testing.T) {
	_, err := Load(write(t, "audit:\n  tls:\n    exceptions:\n    - namespace: homepage\n      service: homepage\n"))
	if err == nil || !strings.Contains(err.Error(), "homepage/homepage") || !strings.Contains(err.Error(), "reason") {
		t.Fatalf("err = %v, want one naming homepage/homepage and reason", err)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), DefaultPath))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}
