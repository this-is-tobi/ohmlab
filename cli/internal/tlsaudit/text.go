package tlsaudit

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// WriteText writes the report for a terminal: what breaks the rule first,
// then what could not be proven, the stale exceptions, and the counts. all
// lists every finding at the end.
func WriteText(w io.Writer, r Report, all bool) error {
	wireguard := "off"
	if r.WireGuard {
		wireguard = "on"
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "context %s: %d ports, WireGuard %s\n", r.Context, len(r.Findings), wireguard)

	violations := r.Violations()
	var unproven, tlsOK []Finding
	ssh := 0
	allowed := map[string]int{}
	for _, f := range r.Findings {
		switch {
		case f.Verdict == VerdictTLS:
			tlsOK = append(tlsOK, f)
		case f.Verdict == VerdictSSH:
			ssh++
		case f.Allowed != "":
			allowed[allowedKind(f.Allowed)]++
		case f.Verdict == VerdictClosed || f.Verdict == VerdictUnknown:
			unproven = append(unproven, f)
		}
	}
	section(tw, "✗", "not allowed", violations)
	section(tw, "?", "not proven", unproven)
	if len(r.Stale) > 0 {
		fmt.Fprintf(tw, "\n! %d stale exception(s), they allow nothing\n", len(r.Stale))
		for _, e := range r.Stale {
			fmt.Fprintf(tw, "  %s/%s %v\t%s\n", e.Namespace, e.Service, e.Ports, e.Reason)
		}
	}

	counts := []string{fmt.Sprintf("%d tls", len(tlsOK))}
	if ssh > 0 {
		counts = append(counts, fmt.Sprintf("%d ssh", ssh))
	}
	kinds := make([]string, 0, len(allowed))
	for k := range allowed {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		counts = append(counts, fmt.Sprintf("%d %s", allowed[k], k))
	}
	fmt.Fprintf(tw, "\n✓ %s\n", strings.Join(counts, ", "))

	if all {
		fmt.Fprintln(tw, "\nall ports")
		for _, f := range r.Findings {
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", f.Verdict, address(f.Target), f.Detail, f.Allowed)
		}
	}
	return tw.Flush()
}

func section(w io.Writer, mark, title string, findings []Finding) {
	if len(findings) == 0 {
		return
	}
	fmt.Fprintf(w, "\n%s %d %s\n", mark, len(findings), title)
	for _, f := range findings {
		fmt.Fprintf(w, "  %s\t%s\t%s\n", f.Verdict, address(f.Target), f.Detail)
	}
}

func address(t Target) string {
	a := fmt.Sprintf("%s/%s:%d", t.Namespace, t.Service, t.Port)
	if t.PortName != "" {
		a += " (" + t.PortName + ")"
	}
	return a
}

// allowedKind groups the counts: metrics and dns by name, every exception
// reason under one heading.
func allowedKind(allowed string) string {
	if allowed == "metrics" || allowed == "dns" {
		return allowed
	}
	return "allowed by an exception"
}
