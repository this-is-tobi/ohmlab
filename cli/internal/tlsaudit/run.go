package tlsaudit

import (
	"context"
	"sort"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"
)

// ForwardFunc opens a way to a pod's port: a dialer and the function that
// closes it.
type ForwardFunc func(ctx context.Context, namespace, pod string, port int32) (DialFunc, func(), error)

// Options tune a run.
type Options struct {
	Concurrency int
	Timeout     time.Duration
}

// Finding is the verdict on one target.
type Finding struct {
	Target
	Verdict Verdict `json:"verdict"`
	Detail  string  `json:"detail,omitempty"`
	Allowed string  `json:"allowed,omitempty"` // "metrics", "dns" or the exception's reason
}

// Run probes every target, Concurrency at a time. A target it cannot reach
// gets an unknown verdict with the reason; nothing stops the run.
func Run(ctx context.Context, targets []Target, forward ForwardFunc, opts Options) []Finding {
	findings := make([]Finding, len(targets))
	var g errgroup.Group
	g.SetLimit(max(opts.Concurrency, 1))
	for i, t := range targets {
		g.Go(func() error {
			findings[i] = probe(ctx, t, forward, opts.Timeout)
			return nil
		})
	}
	_ = g.Wait()
	sort.SliceStable(findings, func(i, j int) bool { return less(findings[i].Target, findings[j].Target) })
	return findings
}

func probe(ctx context.Context, t Target, forward ForwardFunc, timeout time.Duration) Finding {
	f := Finding{Target: t}
	if t.Pod == "" {
		f.Verdict, f.Detail = VerdictUnknown, "no ready endpoint"
		return f
	}
	dial, stop, err := forward(ctx, t.Namespace, t.Pod, t.PodPort)
	if err != nil {
		f.Verdict, f.Detail = VerdictUnknown, "port-forward: "+err.Error()
		return f
	}
	defer stop()
	if t.Port == 5432 || t.PodPort == 5432 || strings.Contains(t.PortName, "postgres") {
		f.Verdict, f.Detail = ProbePostgres(ctx, dial, timeout)
	} else {
		f.Verdict, f.Detail = ProbeTLS(ctx, dial, timeout)
	}
	return f
}
