package vaultseed

import (
	"context"
	"errors"
	"time"

	"github.com/this-is-tobi/ohmlab/utils/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/utils/internal/seed"
	"github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

// KV is what the reconciler asks of Vault. *vault.Client implements it.
type KV interface {
	seed.KV
	KVDestroy(ctx context.Context, mount, path string) error
}

// instrumented counts and times every call, and counts the writes. It is the
// only place the backend traffic is measured, so a write loop shows up as a
// rate of ohmlab_backend_writes_total.
type instrumented struct {
	inner  KV
	m      *metrics.Metrics
	target string
}

func (i *instrumented) observe(op string, start time.Time, err error) {
	result := "ok"
	switch {
	case errors.Is(err, vault.ErrCASMismatch):
		result = "conflict"
	case errors.Is(err, vault.ErrForbidden):
		result = "forbidden"
	case err != nil:
		result = "error"
	}
	i.m.BackendRequests.WithLabelValues(i.target, op, result).Inc()
	i.m.BackendDuration.WithLabelValues(i.target, op).Observe(time.Since(start).Seconds())
}

func (i *instrumented) KVRead(ctx context.Context, mount, path string) (*vault.KVSecret, error) {
	start := time.Now()
	s, err := i.inner.KVRead(ctx, mount, path)
	i.observe("read", start, err)
	return s, err
}

func (i *instrumented) KVWriteCAS(ctx context.Context, mount, path string, data map[string]any, cas int64) (int64, error) {
	start := time.Now()
	v, err := i.inner.KVWriteCAS(ctx, mount, path, data, cas)
	i.observe("write", start, err)
	if err == nil {
		i.m.BackendWrites.WithLabelValues(i.target, kind, "data").Inc()
	}
	return v, err
}

func (i *instrumented) KVSetCustomMetadata(ctx context.Context, mount, path string, md map[string]string) error {
	start := time.Now()
	err := i.inner.KVSetCustomMetadata(ctx, mount, path, md)
	i.observe("metadata", start, err)
	if err == nil {
		i.m.BackendWrites.WithLabelValues(i.target, kind, "metadata").Inc()
	}
	return err
}

func (i *instrumented) KVDestroy(ctx context.Context, mount, path string) error {
	start := time.Now()
	err := i.inner.KVDestroy(ctx, mount, path)
	i.observe("delete", start, err)
	if err == nil {
		i.m.BackendWrites.WithLabelValues(i.target, kind, "delete").Inc()
	}
	return err
}
