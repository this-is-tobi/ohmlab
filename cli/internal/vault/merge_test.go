package vault

import "testing"

func TestDeepMerge(t *testing.T) {
	tests := []struct {
		name     string
		existing map[string]any
		incoming map[string]any
		want     map[string]any
	}{
		{
			name:     "nil existing returns copy of incoming",
			existing: nil,
			incoming: map[string]any{"a": "1"},
			want:     map[string]any{"a": "1"},
		},
		{
			name:     "nil incoming returns copy of existing",
			existing: map[string]any{"a": "1"},
			incoming: nil,
			want:     map[string]any{"a": "1"},
		},
		{
			name:     "existing scalar wins over incoming",
			existing: map[string]any{"a": "keep"},
			incoming: map[string]any{"a": "discard"},
			want:     map[string]any{"a": "keep"},
		},
		{
			name:     "keys only in incoming are added",
			existing: map[string]any{"a": "1"},
			incoming: map[string]any{"b": "2"},
			want:     map[string]any{"a": "1", "b": "2"},
		},
		{
			name: "nested maps merge recursively, existing sub-keys win",
			existing: map[string]any{
				"nested": map[string]any{"keep": "old"},
			},
			incoming: map[string]any{
				"nested": map[string]any{"keep": "new", "add": "yes"},
			},
			want: map[string]any{
				"nested": map[string]any{"keep": "old", "add": "yes"},
			},
		},
		{
			name:     "existing map wins over incoming scalar",
			existing: map[string]any{"a": map[string]any{"x": "1"}},
			incoming: map[string]any{"a": "scalar"},
			want:     map[string]any{"a": map[string]any{"x": "1"}},
		},
		{
			name:     "existing slice wins over incoming slice",
			existing: map[string]any{"a": []any{"1"}},
			incoming: map[string]any{"a": []any{"2", "3"}},
			want:     map[string]any{"a": []any{"1"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DeepMerge(tt.existing, tt.incoming)
			if !Equal(got, tt.want) {
				t.Errorf("DeepMerge() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// The result must be decoupled from both inputs — mutating it must never
// write through into the maps the caller handed in.
func TestDeepMergeCopiesInputs(t *testing.T) {
	existing := map[string]any{"nested": map[string]any{"k": "v"}}
	incoming := map[string]any{"other": map[string]any{"x": "y"}, "list": []any{"a"}}

	got := DeepMerge(existing, incoming)

	got["nested"].(map[string]any)["k"] = "mutated"
	got["other"].(map[string]any)["x"] = "mutated"
	got["list"].([]any)[0] = "mutated"

	if existing["nested"].(map[string]any)["k"] != "v" {
		t.Error("mutating result leaked into existing")
	}
	if incoming["other"].(map[string]any)["x"] != "y" {
		t.Error("mutating result leaked into incoming map")
	}
	if incoming["list"].([]any)[0] != "a" {
		t.Error("mutating result leaked into incoming slice")
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b map[string]any
		want bool
	}{
		{"both empty", map[string]any{}, map[string]any{}, true},
		{"same scalars", map[string]any{"a": "1", "b": true}, map[string]any{"a": "1", "b": true}, true},
		{"different values", map[string]any{"a": "1"}, map[string]any{"a": "2"}, false},
		{"different keys", map[string]any{"a": "1"}, map[string]any{"b": "1"}, false},
		{"different sizes", map[string]any{"a": "1"}, map[string]any{"a": "1", "b": "2"}, false},
		{
			"nested equal",
			map[string]any{"n": map[string]any{"x": []any{"1", "2"}}},
			map[string]any{"n": map[string]any{"x": []any{"1", "2"}}},
			true,
		},
		{
			"nested slice differs in length",
			map[string]any{"n": []any{"1"}},
			map[string]any{"n": []any{"1", "2"}},
			false,
		},
		{
			"nested slice differs in value",
			map[string]any{"n": []any{"1"}},
			map[string]any{"n": []any{"2"}},
			false,
		},
		{
			"map vs scalar",
			map[string]any{"n": map[string]any{}},
			map[string]any{"n": "scalar"},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Equal(tt.a, tt.b); got != tt.want {
				t.Errorf("Equal() = %v, want %v", got, tt.want)
			}
		})
	}
}
