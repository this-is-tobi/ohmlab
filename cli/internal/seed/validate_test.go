package seed

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidateAcceptsAWellFormedDeclaration(t *testing.T) {
	data := tree(
		"admin", tree("password", "<random:24>", "hashed", "<bcrypt:admin.password>"),
		"copy", "<ref:homelab/platforms/production/core/keycloak#clients.x.secret>",
		"pair", tree("s", "<age:secret>", "p", "<age:public>"),
		"hex", "<hex:32>", "id", "<uuid>", "plain", "", "list", []any{"<random:4>", 3},
	)
	if errs := Validate("homelab/platforms/production/core/app", data, &Rotate{Token: "t", Keys: []string{"admin.password", "hex"}}); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestValidateNamesEachProblem(t *testing.T) {
	data := tree(
		"a", "<random:0>",
		"b", "<hex:7>",
		"c", "<ref:homelab/a/b/c>",
		"d", "<bcrypt:nope>",
		"e", tree("f", "<random:oops>"),
	)
	errs := Validate("homelab", data, &Rotate{Token: "", Keys: []string{"plain"}})
	var msgs []string
	for _, e := range errs {
		msgs = append(msgs, e.Error())
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{
		`invalid path "homelab"`, "a: <random:0>", "b: <hex:7>", "c: malformed placeholder <ref:homelab/a/b/c>",
		"d: <bcrypt:nope>", "e.f: malformed placeholder <random:oops>", "rotate.token must not be empty", `rotate.keys: "plain"`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestValidateRejectsEmptyData(t *testing.T) {
	if errs := Validate("homelab/a/b", tree(), nil); len(errs) != 1 {
		t.Errorf("errors: %v", errs)
	}
}

func TestSources(t *testing.T) {
	data := tree(
		"a", "<ref:homelab/x/y/one#k>",
		"n", tree("b", "https://<ref:homelab/x/y/two#k>/p <ref:homelab/x/y/one#k2>"),
		"l", []any{"<ref:homelab/x/y/three#k>"},
	)
	want := []string{"homelab/x/y/one", "homelab/x/y/three", "homelab/x/y/two"}
	if got := Sources(data); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestFindCycle(t *testing.T) {
	tests := []struct {
		name  string
		graph map[string][]string
		start string
		want  []string
	}{
		{"none", map[string][]string{"a": {"b"}, "b": {"c"}}, "a", nil},
		{"self", map[string][]string{"a": {"a"}}, "a", []string{"a", "a"}},
		{"two", map[string][]string{"a": {"b"}, "b": {"a"}}, "a", []string{"a", "b", "a"}},
		{"three", map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"a"}}, "b", []string{"b", "c", "a", "b"}},
		{"a cycle that start only leads into", map[string][]string{"a": {"b"}, "b": {"c"}, "c": {"b"}}, "a", nil},
		{"diamond is not a cycle", map[string][]string{"a": {"b", "c"}, "b": {"d"}, "c": {"d"}}, "a", nil},
		{"unknown start", map[string][]string{"a": {"b"}}, "z", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FindCycle(tt.graph, tt.start)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateRejectsAKeyNameWithADot(t *testing.T) {
	data := tree("clients", tree("grafana.example.com", tree("secret", "<random:8>")), "ok", "x")
	errs := Validate("homelab/p/c/x", data, nil)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "clients.grafana.example.com") || !strings.Contains(errs[0].Error(), "dot") {
		t.Errorf("errors: %v", errs)
	}
}
