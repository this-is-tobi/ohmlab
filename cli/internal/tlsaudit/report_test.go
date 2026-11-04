package tlsaudit

import (
	"reflect"
	"testing"

	"github.com/this-is-tobi/ohmlab/cli/internal/config"
)

func finding(ns, svc string, p int32, v Verdict) Finding {
	return Finding{Target: Target{Namespace: ns, Service: svc, Port: p}, Verdict: v}
}

func TestEvaluateAllowsMetricsDNSAndExceptions(t *testing.T) {
	metrics := finding("monitoring", "exporter", 9100, VerdictPlain)
	metrics.Metrics = true
	findings := []Finding{
		metrics,
		finding("kube-system", "kube-dns", 53, VerdictSilent),
		finding("homepage", "homepage", 3000, VerdictPlain),
		finding("keycloak", "keycloak", 8080, VerdictPlain),
	}
	exceptions := []config.TLSException{{Namespace: "homepage", Service: "homepage", Reason: "serves HTTP only"}}
	got, stale := Evaluate(findings, exceptions)
	allowed := []string{got[0].Allowed, got[1].Allowed, got[2].Allowed, got[3].Allowed}
	if want := []string{"metrics", "dns", "serves HTTP only", ""}; !reflect.DeepEqual(allowed, want) {
		t.Errorf("allowed %q, want %q", allowed, want)
	}
	if len(stale) != 0 {
		t.Errorf("stale %+v, want none", stale)
	}
}

func TestEvaluateReportsStaleExceptions(t *testing.T) {
	findings := []Finding{finding("app", "web", 443, VerdictTLS), finding("app", "web", 80, VerdictPlain)}
	gone := config.TLSException{Namespace: "gone", Service: "gone", Reason: "removed app"}
	nowTLS := config.TLSException{Namespace: "app", Service: "web", Ports: []int32{443}, Reason: "was plain"}
	used := config.TLSException{Namespace: "app", Service: "web", Ports: []int32{80}, Reason: "redirects only"}
	got, stale := Evaluate(findings, []config.TLSException{gone, nowTLS, used})
	if want := []config.TLSException{gone, nowTLS}; !reflect.DeepEqual(stale, want) {
		t.Errorf("stale %+v, want %+v", stale, want)
	}
	if got[0].Allowed != "" || got[1].Allowed != "redirects only" {
		t.Errorf("allowed %q and %q, want none and the exception's reason", got[0].Allowed, got[1].Allowed)
	}
}

func TestViolationsIgnoreClosedAndUnknown(t *testing.T) {
	allowed := finding("app", "silent", 6379, VerdictSilent)
	allowed.Allowed = "until Redis TLS"
	r := Report{Findings: []Finding{
		finding("app", "plain", 80, VerdictPlain),
		finding("app", "closed", 443, VerdictClosed),
		finding("app", "unknown", 80, VerdictUnknown),
		finding("db", "pg-rw", 5432, VerdictTLSOptional),
		allowed,
		finding("app", "tls", 443, VerdictTLS),
	}}
	var got []string
	for _, f := range r.Violations() {
		got = append(got, f.Service)
	}
	if want := []string{"plain", "pg-rw"}; !reflect.DeepEqual(got, want) {
		t.Errorf("violations %v, want %v", got, want)
	}
}

// SSH encrypts by itself: nothing to allow, and an exception for it is stale.
func TestEvaluateTreatsSSHAsEncrypted(t *testing.T) {
	findings := []Finding{finding("git", "git-ssh", 22, VerdictSSH)}
	out, stale := Evaluate(findings, []config.TLSException{{Namespace: "git", Service: "git-ssh", Reason: "SSH"}})
	if out[0].Allowed != "" || len(stale) != 1 {
		t.Errorf("allowed %q, stale %v; want nothing allowed and the exception stale", out[0].Allowed, stale)
	}
	if v := (Report{Findings: out}).Violations(); len(v) != 0 {
		t.Errorf("violations %v, want none", v)
	}
}
