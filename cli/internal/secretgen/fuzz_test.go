package secretgen

import (
	"strings"
	"testing"
)

// The template parser reads text an author controls. Whatever it is given, it
// must not panic, must not hang, and must say the same thing twice.
func FuzzTemplates(f *testing.F) {
	for _, seed := range []string{
		"", "<random:24>", "<hex:16>", "<uuid>", "<age:secret>", "<bcrypt:a.b>",
		"<ref:homelab/a/b/c#k>", "<ref:<ref:x#y>#z>", "<random:99999999999999999999>",
		"<hex:", "<<random:3>>", "<ref:#>", "<bcrypt:>", strings.Repeat("<uuid>", 50),
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, s string) {
		kind := Classify(s)
		if kind != Classify(s) {
			t.Fatal("Classify is not deterministic")
		}
		_ = Malformed(s)
		_ = RefSources(s)
		_, _ = BcryptTarget(s)
		problems := Problems(s)
		if len(problems) == 0 && kind == KindGenerated {
			// Only well-formed generated placeholders: rendering must succeed.
			r := &Renderer{Ref: func(string, string) (string, error) { return "v", nil }}
			if _, err := r.Render(s); err != nil {
				t.Fatalf("Problems found nothing but Render failed on %q: %v", s, err)
			}
		}
	})
}
