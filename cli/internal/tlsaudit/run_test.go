package tlsaudit

import (
	"context"
	"errors"
	"testing"
	"time"
)

var opts = Options{Concurrency: 2, Timeout: probeTimeout}

func TestRunNoEndpointIsUnknown(t *testing.T) {
	called := false
	forward := func(context.Context, string, string, int32) (DialFunc, func(), error) {
		called = true
		return nil, nil, errors.New("unreachable")
	}
	got := Run(context.Background(), []Target{{Namespace: "app", Service: "web", Port: 80}}, forward, opts)
	if len(got) != 1 || got[0].Verdict != VerdictUnknown || got[0].Detail != "no ready endpoint" || called {
		t.Errorf("got %+v (forward called: %v), want unknown, no ready endpoint, no forward", got, called)
	}
}

func TestRunForwardErrorIsUnknown(t *testing.T) {
	forward := func(context.Context, string, string, int32) (DialFunc, func(), error) {
		return nil, nil, errors.New(`pods "web-1" is forbidden`)
	}
	got := Run(context.Background(), []Target{{Namespace: "app", Service: "web", Port: 80, Pod: "web-1", PodPort: 8080}}, forward, opts)
	if len(got) != 1 || got[0].Verdict != VerdictUnknown || got[0].Detail != `port-forward: pods "web-1" is forbidden` {
		t.Errorf("got %+v, want unknown with the port-forward error", got)
	}
}

func TestRunProbesPostgresPorts(t *testing.T) {
	addr := pgServer(t, 'N', nil)
	stopped := false
	forward := func(_ context.Context, ns, pod string, p int32) (DialFunc, func(), error) {
		if ns != "db" || pod != "pg-1" || p != 5432 {
			t.Errorf("forward to %s/%s:%d", ns, pod, p)
		}
		return dialer(addr), func() { stopped = true }, nil
	}
	got := Run(context.Background(), []Target{{Namespace: "db", Service: "pg-rw", Port: 5432, Pod: "pg-1", PodPort: 5432}}, forward,
		Options{Concurrency: 1, Timeout: time.Second})
	if len(got) != 1 || got[0].Verdict != VerdictPlain || got[0].Detail != "no TLS offered" || !stopped {
		t.Errorf("got %+v (stopped: %v), want the Postgres probe's plain verdict and the forward stopped", got, stopped)
	}
}
