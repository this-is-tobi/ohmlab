package seed

import (
	"fmt"
	"sort"
	"strings"

	"github.com/this-is-tobi/ohmlab/utils/internal/secretgen"
)

// Validate checks a declaration without reading or writing anything: the
// path, every template, the targets of <bcrypt:...>, the keys a rotation
// names. Each problem names the key, never a value. The controller stalls an
// object on any problem; `ohmlab validate` reports them in CI.
func Validate(path string, data map[string]any, rotate *Rotate) []error {
	var errs []error
	if _, _, err := SplitPath(path); err != nil {
		errs = append(errs, err)
	}
	if len(data) == 0 {
		errs = append(errs, fmt.Errorf("data must declare at least one key"))
	}

	for _, key := range dottedKeys(data, "") {
		errs = append(errs, fmt.Errorf("%s: a key name must not contain a dot: dots separate the levels in rotate.keys, <bcrypt:...> and status.managedKeys", key))
	}

	walkStrings(data, "", func(key, s string) {
		for _, p := range secretgen.Problems(s) {
			errs = append(errs, fmt.Errorf("%s: %s", key, p))
		}
		if target, ok := secretgen.BcryptTarget(s); ok {
			if _, found := stringAt(data, target); !found {
				errs = append(errs, fmt.Errorf("%s: <bcrypt:%s> hashes a key that is not declared as a text value", key, target))
			}
		}
	})

	if rotate != nil {
		if rotate.Token == "" {
			errs = append(errs, fmt.Errorf("rotate.token must not be empty"))
		}
		generated := generatedLeaves(data)
		for _, k := range rotate.Keys {
			if !generated[k] {
				errs = append(errs, fmt.Errorf("rotate.keys: %q is not a generated key of this path", k))
			}
		}
	}
	return errs
}

// walkStrings calls fn for every string leaf, including list elements, in key order.
func walkStrings(m map[string]any, prefix string, fn func(key, s string)) {
	for _, k := range sortedKeys(m) {
		key := join(prefix, k)
		switch v := m[k].(type) {
		case string:
			fn(key, v)
		case map[string]any:
			walkStrings(v, key, fn)
		case []any:
			for _, item := range v {
				if s, ok := item.(string); ok {
					fn(key, s)
				}
			}
		}
	}
}

// Sources returns the "<mount>/<path>" of every reference in the tree: the
// paths this declaration depends on.
func Sources(data map[string]any) []string {
	set := map[string]bool{}
	walkStrings(data, "", func(_, s string) {
		for _, src := range secretgen.RefSources(s) {
			set[src] = true
		}
	})
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// FindCycle reports a reference cycle that goes through start. graph maps a
// path to the paths its declaration references. A path that references itself
// is a cycle. The result lists the paths of the cycle starting and ending at
// start; it is nil when start is on no cycle.
func FindCycle(graph map[string][]string, start string) []string {
	var path []string
	onPath := map[string]bool{}
	done := map[string]bool{}
	var visit func(n string) []string
	visit = func(n string) []string {
		if onPath[n] {
			if n != start {
				return nil // a cycle that does not include start is not start's to report
			}
			return append(append([]string{}, path...), start)
		}
		if done[n] {
			return nil
		}
		onPath[n] = true
		path = append(path, n)
		for _, next := range graph[n] {
			if c := visit(next); c != nil {
				return c
			}
		}
		path = path[:len(path)-1]
		onPath[n] = false
		done[n] = true
		return nil
	}
	return visit(start)
}

// DescribeCycle renders a cycle for a condition message.
func DescribeCycle(cycle []string) string { return strings.Join(cycle, " -> ") }

// dottedKeys lists the paths of keys whose own name contains a dot.
func dottedKeys(m map[string]any, prefix string) []string {
	var out []string
	for _, k := range sortedKeys(m) {
		key := join(prefix, k)
		if strings.Contains(k, ".") {
			out = append(out, key)
		}
		if sub, ok := m[k].(map[string]any); ok {
			out = append(out, dottedKeys(sub, key)...)
		}
	}
	return out
}
