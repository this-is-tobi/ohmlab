package secretgen

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestRenderPlaceholders(t *testing.T) {
	r := &Renderer{}
	got, err := r.Render("a-<random:8>-<hex:8>-<uuid>")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "<") || len(got) != len("a---")+8+8+36 {
		t.Errorf("Render left placeholders or the wrong length: %q", got)
	}
	a, _ := r.Render("<random:12>")
	b, _ := r.Render("<random:12>")
	if a == b {
		t.Error("every <random> must be its own value")
	}
}

func TestRenderAgeIsAPair(t *testing.T) {
	r := &Renderer{}
	secret, err := r.Render("<age:secret>")
	if err != nil {
		t.Fatal(err)
	}
	public, _ := r.Render("<age:public>")
	back, err := AgePairFromSecret(secret)
	if err != nil || back.Public != public {
		t.Errorf("public %q is not the pair of secret (%v)", public, err)
	}
}

func TestRenderAgeReusesAGivenPair(t *testing.T) {
	p, _ := NewAgePair()
	r := &Renderer{Age: &p}
	got, _ := r.Render("<age:public>")
	if got != p.Public {
		t.Errorf("got %q, want the given pair's public key", got)
	}
}

func TestRenderRef(t *testing.T) {
	var asked [][2]string
	r := &Renderer{Ref: func(path, key string) (string, error) {
		asked = append(asked, [2]string{path, key})
		return "V", nil
	}}
	got, err := r.Render("https://<ref:homelab/a/b/c#host.name>/x")
	if err != nil || got != "https://V/x" {
		t.Fatalf("got %q, %v", got, err)
	}
	if !reflect.DeepEqual(asked, [][2]string{{"homelab/a/b/c", "host.name"}}) {
		t.Errorf("asked %v", asked)
	}
}

func TestRenderRefErrors(t *testing.T) {
	if _, err := (&Renderer{}).Render("<ref:homelab/a/b/c#k>"); err == nil {
		t.Error("a reference without a resolver must fail")
	}
	r := &Renderer{Ref: func(string, string) (string, error) { return "", ErrRefNotFound }}
	_, err := r.Render("<ref:homelab/a/b/c#k>")
	if !errors.Is(err, ErrRefNotFound) {
		t.Errorf("got %v, want ErrRefNotFound", err)
	}
}

func TestRenderBadLengths(t *testing.T) {
	for _, in := range []string{"<random:0>", "<hex:7>", "<hex:0>"} {
		if _, err := (&Renderer{}).Render(in); err == nil {
			t.Errorf("Render(%q) must fail", in)
		}
	}
}

func TestRenderLeavesBcryptAlone(t *testing.T) {
	got, err := (&Renderer{}).Render("<bcrypt:admin.password>")
	if err != nil || got != "<bcrypt:admin.password>" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestBcryptTarget(t *testing.T) {
	if k, ok := BcryptTarget("<bcrypt:admin.password>"); !ok || k != "admin.password" {
		t.Errorf("got %q, %v", k, ok)
	}
	if _, ok := BcryptTarget("plain"); ok {
		t.Error("plain text is not a bcrypt target")
	}
}

func TestRefSources(t *testing.T) {
	got := RefSources("<ref:homelab/a/b/c#x> and <ref:homelab/d/e/f#y.z>")
	if !reflect.DeepEqual(got, []string{"homelab/a/b/c", "homelab/d/e/f"}) {
		t.Errorf("got %v", got)
	}
}

func TestMalformed(t *testing.T) {
	tests := map[string][]string{
		"<random:24>":                        nil,
		"https://example.com/<not-ours>":     nil,
		"<random:abc>":                       {"<random:abc>"},
		"<ref:homelab/a/b/c>":                {"<ref:homelab/a/b/c>"},
		"<age:both>":                         {"<age:both>"},
		"ok <uuid> and bad <hex:>":           {"<hex:>"},
		"<bcrypt:admin.password> <random:4>": nil,
	}
	for in, want := range tests {
		if got := Malformed(in); !reflect.DeepEqual(got, want) {
			t.Errorf("Malformed(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNavigateJSON(t *testing.T) {
	data := map[string]any{
		"a": map[string]any{"b": "x", "n": json.Number("42"), "f": 1.5, "t": true, "l": []any{"p"}},
	}
	tests := []struct{ path, want string }{
		{"a.b", "x"}, {"a.n", "42"}, {"a.f", "1.5"}, {"a.t", "true"}, {"a.l", `["p"]`},
		{"a.missing", ""}, {"a.b.c", ""}, {"nope", ""},
	}
	for _, tt := range tests {
		if got := NavigateJSON(data, tt.path); got != tt.want {
			t.Errorf("NavigateJSON(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestProblems(t *testing.T) {
	tests := map[string]int{
		"<random:24>":               0,
		"<hex:16> and <uuid>":       0,
		"<random:abc>":              1,
		"<random:0>":                1,
		"<random:99999>":            1,
		"<random:1>":                0,
		"<random:4096>":             0,
		"<random:4097>":             1,
		"<hex:2>":                   0,
		"<hex:4096>":                0,
		"<hex:4098>":                1,
		"<hex:7>":                   1,
		"<hex:0>":                   1,
		"<random:0> <hex:7>":        2,
		"<random:0> <random:0>":     2,
		"<hex:7> <hex:9>":           2,
		"<ref:homelab/a/b/c>":       1,
		"nothing to see":            0,
		"<bcrypt:a.b> <age:public>": 0,
	}
	for in, want := range tests {
		if got := Problems(in); len(got) != want {
			t.Errorf("Problems(%q) = %v, want %d", in, got, want)
		}
	}
}
