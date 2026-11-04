package tlsaudit

import (
	"slices"

	"github.com/this-is-tobi/ohmlab/cli/internal/config"
)

// Report is the outcome of an audit.
type Report struct {
	Context   string                `json:"context"`
	WireGuard bool                  `json:"wireguard"` // Cilium encrypts the traffic between nodes
	Findings  []Finding             `json:"findings"`
	Stale     []config.TLSException `json:"staleExceptions,omitempty"`
}

// Evaluate sets what allows each finding that is not TLS: a ServiceMonitor
// naming the port (metrics), port 53 (dns), or an exception. It returns the
// findings and the exceptions that allow nothing, which include those whose
// ports speak TLS now.
func Evaluate(findings []Finding, exceptions []config.TLSException) ([]Finding, []config.TLSException) {
	used := make([]bool, len(exceptions))
	out := slices.Clone(findings)
	for i := range out {
		f := &out[i]
		if f.Verdict == VerdictTLS || f.Verdict == VerdictSSH {
			continue
		}
		switch {
		case f.Metrics:
			f.Allowed = "metrics"
		case f.Port == 53:
			f.Allowed = "dns"
		default:
			for j, e := range exceptions {
				if e.Namespace == f.Namespace && e.Service == f.Service && (len(e.Ports) == 0 || slices.Contains(e.Ports, f.Port)) {
					f.Allowed, used[j] = e.Reason, true
					break
				}
			}
		}
	}
	var stale []config.TLSException
	for j, e := range exceptions {
		if !used[j] {
			stale = append(stale, e)
		}
	}
	return out, stale
}

// Violations are the findings the rule does not allow: answered in plaintext,
// stayed silent, or accepted plaintext next to TLS, with nothing allowing it.
// A closed or unknown port is not proven either way and is not one.
func (r Report) Violations() []Finding {
	var out []Finding
	for _, f := range r.Findings {
		switch f.Verdict {
		case VerdictPlain, VerdictSilent, VerdictTLSOptional:
			if f.Allowed == "" {
				out = append(out, f)
			}
		}
	}
	return out
}
