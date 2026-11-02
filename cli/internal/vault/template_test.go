package vault

import (
	"regexp"
	"strings"
	"testing"

	"filippo.io/age"
	"golang.org/x/crypto/bcrypt"
)

func process(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	engine := NewTemplateEngine(nil)
	result, err := engine.Process(data)
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}
	return result
}

func TestProcessRandom(t *testing.T) {
	result := process(t, map[string]any{
		"a":        "<random:32>",
		"b":        "<random:32>",
		"prefixed": "pw-<random:8>-end",
	})

	a := result["a"].(string)
	b := result["b"].(string)

	if len(a) != 32 || len(b) != 32 {
		t.Fatalf("expected 32-char values, got %d and %d", len(a), len(b))
	}
	if a == b {
		t.Error("two <random:32> placeholders produced the same value")
	}
	if !regexp.MustCompile(`^[A-Za-z0-9]+$`).MatchString(a) {
		t.Errorf("random value contains non-alphanumeric characters: %q", a)
	}

	prefixed := result["prefixed"].(string)
	if !regexp.MustCompile(`^pw-[A-Za-z0-9]{8}-end$`).MatchString(prefixed) {
		t.Errorf("inline placeholder not replaced correctly: %q", prefixed)
	}
}

func TestProcessRandomZeroLength(t *testing.T) {
	engine := NewTemplateEngine(nil)
	if _, err := engine.Process(map[string]any{"a": "<random:0>"}); err == nil {
		t.Error("expected error for <random:0>, got nil")
	}
}

func TestProcessHex(t *testing.T) {
	result := process(t, map[string]any{
		"a":        "<hex:64>",
		"b":        "<hex:64>",
		"prefixed": "key-<hex:8>-end",
	})

	a := result["a"].(string)
	b := result["b"].(string)

	if len(a) != 64 || len(b) != 64 {
		t.Fatalf("expected 64-char values, got %d and %d", len(a), len(b))
	}
	if a == b {
		t.Error("two <hex:64> placeholders produced the same value")
	}
	if !regexp.MustCompile(`^[0-9a-f]+$`).MatchString(a) {
		t.Errorf("hex value contains non-hex characters: %q", a)
	}

	prefixed := result["prefixed"].(string)
	if !regexp.MustCompile(`^key-[0-9a-f]{8}-end$`).MatchString(prefixed) {
		t.Errorf("inline placeholder not replaced correctly: %q", prefixed)
	}
}

func TestProcessHexOddLength(t *testing.T) {
	engine := NewTemplateEngine(nil)
	if _, err := engine.Process(map[string]any{"a": "<hex:7>"}); err == nil {
		t.Error("expected error for <hex:7>, got nil")
	}
}

func TestProcessUUID(t *testing.T) {
	result := process(t, map[string]any{"a": "<uuid>", "b": "<uuid>"})

	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	a := result["a"].(string)
	if !uuidRe.MatchString(a) {
		t.Errorf("not a UUID v4: %q", a)
	}
	if a == result["b"].(string) {
		t.Error("two <uuid> placeholders produced the same value")
	}
}

func TestProcessAgeKeypair(t *testing.T) {
	result := process(t, map[string]any{
		"secret": "<age:secret>",
		"public": "<age:public>",
	})

	secret := result["secret"].(string)
	public := result["public"].(string)

	if !strings.HasPrefix(secret, "AGE-SECRET-KEY-") {
		t.Fatalf("unexpected age secret format: %q", secret)
	}
	// The two placeholders must form a PAIR within one Process call.
	identity, err := age.ParseX25519Identity(secret)
	if err != nil {
		t.Fatalf("generated age secret does not parse: %v", err)
	}
	if identity.Recipient().String() != public {
		t.Error("<age:public> is not the recipient of <age:secret>")
	}
}

func TestProcessAgeKeypairResetsBetweenCalls(t *testing.T) {
	engine := NewTemplateEngine(nil)
	first, err := engine.Process(map[string]any{"s": "<age:secret>"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := engine.Process(map[string]any{"s": "<age:secret>"})
	if err != nil {
		t.Fatal(err)
	}
	if first["s"] == second["s"] {
		t.Error("age keypair was reused across Process calls")
	}
}

func TestProcessBcrypt(t *testing.T) {
	result := process(t, map[string]any{
		"password": "<random:16>",
		"hash":     "<bcrypt:password>",
		"nested": map[string]any{
			"admin": map[string]any{"password": "plain-text"},
		},
		"nestedHash": "<bcrypt:nested.admin.password>",
	})

	password := result["password"].(string)
	hash := result["hash"].(string)
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		t.Errorf("<bcrypt:password> does not verify against the generated password: %v", err)
	}

	nestedHash := result["nestedHash"].(string)
	if err := bcrypt.CompareHashAndPassword([]byte(nestedHash), []byte("plain-text")); err != nil {
		t.Errorf("<bcrypt:...> with dotted path does not verify: %v", err)
	}
}

func TestProcessBcryptMissingField(t *testing.T) {
	engine := NewTemplateEngine(nil)
	if _, err := engine.Process(map[string]any{"hash": "<bcrypt:nope>"}); err == nil {
		t.Error("expected error for <bcrypt:...> referencing a missing field, got nil")
	}
}

func TestProcessRefWithoutClient(t *testing.T) {
	engine := NewTemplateEngine(nil)
	if _, err := engine.Process(map[string]any{"a": "<ref:mount/path#key>"}); err == nil {
		t.Error("expected error for <ref:...> with no Vault client, got nil")
	}
}

func TestProcessNestedStructures(t *testing.T) {
	result := process(t, map[string]any{
		"plain":  "unchanged",
		"number": 42,
		"bool":   true,
		"list":   []any{"<random:4>", "static", map[string]any{"deep": "<uuid>"}},
		"map":    map[string]any{"inner": "<random:6>"},
	})

	if result["plain"] != "unchanged" || result["number"] != 42 || result["bool"] != true {
		t.Error("non-placeholder values were altered")
	}
	list := result["list"].([]any)
	if len(list[0].(string)) != 4 {
		t.Errorf("placeholder in list not processed: %q", list[0])
	}
	if list[1] != "static" {
		t.Errorf("static list value altered: %q", list[1])
	}
	if deep := list[2].(map[string]any)["deep"].(string); strings.Contains(deep, "<uuid>") {
		t.Errorf("placeholder in nested map inside list not processed: %q", deep)
	}
	if inner := result["map"].(map[string]any)["inner"].(string); len(inner) != 6 {
		t.Errorf("placeholder in nested map not processed: %q", inner)
	}
}

func TestNavigateJSON(t *testing.T) {
	data := map[string]any{
		"str":  "value",
		"bool": true,
		"num":  float64(3),
		"nested": map[string]any{
			"deep": map[string]any{"key": "found"},
		},
		"complex": map[string]any{"a": "b"},
	}

	tests := []struct {
		path string
		want string
	}{
		{"str", "value"},
		{"bool", "true"},
		{"num", "3"},
		{"nested.deep.key", "found"},
		{"missing", ""},
		{"str.not-a-map", ""},
		{"nested.missing", ""},
		{"complex", `{"a":"b"}`},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := navigateJSON(data, tt.path); got != tt.want {
				t.Errorf("navigateJSON(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
