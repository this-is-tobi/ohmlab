package seed

import (
	"context"
	"errors"
	"fmt"

	"github.com/this-is-tobi/ohmlab/utils/internal/secretgen"
	"github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

// KV is the part of Vault the engine uses. *vault.Client implements it.
type KV interface {
	KVRead(ctx context.Context, mount, path string) (*vault.KVSecret, error)
	KVWriteCAS(ctx context.Context, mount, path string, data map[string]any, cas int64) (int64, error)
	KVSetCustomMetadata(ctx context.Context, mount, path string, md map[string]string) error
}

// Options tune one Run.
type Options struct {
	// ObserveOnly plans without writing anything.
	ObserveOnly bool
	// Attempts is how many times a lost compare-and-set race is re-read and
	// re-planned. Zero means three.
	Attempts int
}

// Result is what a Run decided and did.
type Result struct {
	Plan Plan
	// Version is the KV version after the run: the new one when data was written.
	Version         int64
	DataWritten     bool
	MetadataWritten bool
}

// Run reads one path, plans it, and writes the plan: the data first with a
// compare-and-set on the version it read, then the markers. A write that loses
// a race re-reads and re-plans, so an edit made between the read and the write
// is kept, never overwritten.
func Run(ctx context.Context, kv KV, fullPath string, spec Spec, opts Options) (Result, error) {
	mount, path, err := SplitPath(fullPath)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrInvalidPath, err)
	}
	attempts := opts.Attempts
	if attempts <= 0 {
		attempts = 3
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		sec, err := kv.KVRead(ctx, mount, path)
		if err != nil {
			return Result{}, err
		}
		obs := Observed{Exists: sec.Exists, Version: sec.Version, Data: sec.Data, Metadata: sec.Metadata}
		plan, err := Compute(ctx, Input{Spec: spec, Observed: obs, Refs: &kvRefs{kv: kv, cache: map[string]*vault.KVSecret{}}})
		if err != nil {
			return Result{}, err
		}
		res := Result{Plan: plan, Version: sec.Version}
		if plan.Blocked != "" || plan.Empty() || opts.ObserveOnly {
			return res, nil
		}

		if plan.WriteData {
			v, err := kv.KVWriteCAS(ctx, mount, path, plan.Data, plan.CAS)
			if errors.Is(err, vault.ErrCASMismatch) {
				lastErr = err
				continue
			}
			if err != nil {
				return res, err
			}
			res.Version, res.DataWritten = v, true
		}
		if plan.WriteMetadata {
			if err := kv.KVSetCustomMetadata(ctx, mount, path, plan.Metadata); err != nil {
				return res, err
			}
			res.MetadataWritten = true
		}
		return res, nil
	}
	return Result{}, fmt.Errorf("path changed %d times while it was being written: %w", attempts, lastErr)
}

// kvRefs resolves references by reading their source path, once per run.
type kvRefs struct {
	kv    KV
	cache map[string]*vault.KVSecret
}

func (r *kvRefs) Lookup(ctx context.Context, path, keyPath string) (string, error) {
	sec, ok := r.cache[path]
	if !ok {
		mount, rest, err := SplitPath(path)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrInvalidPath, err)
		}
		if sec, err = r.kv.KVRead(ctx, mount, rest); err != nil {
			return "", err
		}
		r.cache[path] = sec
	}
	if !sec.Exists {
		return "", secretgen.ErrRefNotFound
	}
	if v := secretgen.NavigateJSON(sec.Data, keyPath); v != "" {
		return v, nil
	}
	return "", secretgen.ErrRefNotFound
}
