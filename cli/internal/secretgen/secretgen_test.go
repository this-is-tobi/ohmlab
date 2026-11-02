package secretgen

import (
	"regexp"
	"strings"
	"testing"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		in   string
		want Kind
	}{
		{"", KindLiteral},
		{"plain text", KindLiteral},
		{"https://example.com/<not-a-placeholder>", KindLiteral},
		{"<random:24>", KindGenerated},
		{"prefix-<hex:16>-suffix", KindGenerated},
		{"<uuid>", KindGenerated},
		{"<age:secret>", KindGenerated},
		{"<age:public>", KindGenerated},
		{"<ref:homelab/platforms/production/core/keycloak#clients.x.clientSecret>", KindRef},
		{"https://<ref:homelab/a/b/c#host>/path", KindRef},
		{"<bcrypt:admin.password>", KindBcrypt},
		// A generated placeholder wins: the value is made once and kept.
		{"<ref:homelab/a/b/c#k>-<random:4>", KindGenerated},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := Classify(tt.in); got != tt.want {
				t.Errorf("Classify(%q) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestRandom(t *testing.T) {
	a, err := Random(24)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Random(24)
	if len(a) != 24 || !regexp.MustCompile(`^[A-Za-z0-9]+$`).MatchString(a) {
		t.Errorf("Random(24) = %q", a)
	}
	if a == b {
		t.Error("two calls returned the same value")
	}
	for _, n := range []int{1, MaxLength} {
		if v, err := Random(n); err != nil || len(v) != n {
			t.Errorf("Random(%d) = %d chars, %v", n, len(v), err)
		}
	}
	for _, n := range []int{0, -1, MaxLength + 1} {
		if _, err := Random(n); err == nil {
			t.Errorf("Random(%d) must fail", n)
		}
	}
}

func TestHex(t *testing.T) {
	h, err := Hex(32)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 32 || !regexp.MustCompile(`^[0-9a-f]+$`).MatchString(h) {
		t.Errorf("Hex(32) = %q", h)
	}
	for _, n := range []int{2, MaxLength} {
		if v, err := Hex(n); err != nil || len(v) != n {
			t.Errorf("Hex(%d) = %d chars, %v", n, len(v), err)
		}
	}
	for _, n := range []int{0, -2, 7, MaxLength + 2} {
		if _, err := Hex(n); err == nil {
			t.Errorf("Hex(%d) must fail", n)
		}
	}
}

func TestUUIDShape(t *testing.T) {
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(UUID()) {
		t.Error("not a v4 UUID")
	}
}

func TestAgePairRoundTrip(t *testing.T) {
	p, err := NewAgePair()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p.Secret, "AGE-SECRET-KEY-") || !strings.HasPrefix(p.Public, "age1") {
		t.Fatalf("unexpected pair %+v", p)
	}
	back, err := AgePairFromSecret(p.Secret)
	if err != nil {
		t.Fatal(err)
	}
	if back != p {
		t.Errorf("pair rebuilt from the secret differs: %+v vs %+v", back, p)
	}
	if _, err := AgePairFromSecret("not a key"); err == nil {
		t.Error("a malformed secret must fail")
	}
}

func TestBcrypt(t *testing.T) {
	h, err := Bcrypt("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if !BcryptMatches(h, "hunter2") || BcryptMatches(h, "hunter3") {
		t.Error("BcryptMatches does not tell the right password from a wrong one")
	}
	if BcryptMatches("not a hash", "x") {
		t.Error("a malformed hash must not match")
	}
}
