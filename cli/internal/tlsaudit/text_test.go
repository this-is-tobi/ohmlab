package tlsaudit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/this-is-tobi/ohmlab/cli/internal/config"
)

func TestWriteTextListsViolationsFirst(t *testing.T) {
	r := Report{
		Context: "homelab",
		Findings: []Finding{
			finding("traefik", "traefik", 443, VerdictClosed),
			finding("keycloak", "keycloak", 8080, VerdictPlain),
			finding("vault", "vault", 8200, VerdictTLS),
		},
		Stale: []config.TLSException{{Namespace: "gone", Service: "gone", Ports: []int32{80}, Reason: "removed app"}},
	}
	var out bytes.Buffer
	if err := WriteText(&out, r, false); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	violation := strings.Index(text, "keycloak/keycloak:8080")
	closed := strings.Index(text, "traefik/traefik:443")
	if violation < 0 || closed < 0 || violation > closed {
		t.Errorf("want the violation listed before the closed port:\n%s", text)
	}
	for _, want := range []string{"homelab", "WireGuard off", "stale", "gone/gone", "1 tls"} {
		if !strings.Contains(text, want) {
			t.Errorf("want %q in:\n%s", want, text)
		}
	}
	if strings.Contains(text, "vault/vault:8200") {
		t.Errorf("a TLS port is listed without --all:\n%s", text)
	}
}

func TestWriteTextCountsSSH(t *testing.T) {
	var out bytes.Buffer
	r := Report{Context: "c", Findings: []Finding{finding("git", "git-ssh", 22, VerdictSSH), finding("vault", "vault", 8200, VerdictTLS)}}
	if err := WriteText(&out, r, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1 tls, 1 ssh") {
		t.Errorf("want the SSH port counted:\n%s", out.String())
	}
}
