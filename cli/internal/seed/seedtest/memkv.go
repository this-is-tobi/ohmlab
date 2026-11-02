// Package seedtest holds an in-memory KV v2 with the semantics the engine
// relies on, so reconcile tests need no Vault. The integration tests check the
// same semantics against a real server.
package seedtest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

type entry struct {
	version int64
	deleted bool
	data    map[string]any
	meta    map[string]string
}

// MemKV is a KV v2 mount set in memory, safe for concurrent use.
type MemKV struct {
	mu      sync.Mutex
	entries map[string]*entry

	// BeforeWrite, when set, runs once at the start of the next data write, with
	// the lock released: tests use it to edit a path between a read and a write.
	BeforeWrite func()

	reads, dataWrites, metaWrites int
	fail                          map[string]error
	destroyErr                    error
	destroys                      int
}

// New returns an empty MemKV.
func New() *MemKV { return &MemKV{entries: map[string]*entry{}, fail: map[string]error{}} }

func key(mount, path string) string { return mount + "/" + path }

func clone(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	b, _ := json.Marshal(m)
	var out map[string]any
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	_ = d.Decode(&out)
	return out
}

// FailReads makes every read of "<mount>/<path>" return err, until ClearFailures.
func (k *MemKV) FailReads(full string, err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.fail[full] = err
}

// FailDestroy makes KVDestroy return err; nil restores it.
func (k *MemKV) FailDestroy(err error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.destroyErr = err
}

// ClearFailures stops every injected read failure.
func (k *MemKV) ClearFailures() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.fail = map[string]error{}
}

// Destroys returns how many paths KVDestroy removed.
func (k *MemKV) Destroys() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.destroys
}

// Counters returns how many reads, data writes and metadata writes happened.
func (k *MemKV) Counters() (reads, dataWrites, metaWrites int) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.reads, k.dataWrites, k.metaWrites
}

// Put writes a new version the way an outside user would, bypassing counters.
func (k *MemKV) Put(full string, data map[string]any, meta map[string]string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	e := k.entries[full]
	if e == nil {
		e = &entry{}
		k.entries[full] = e
	}
	e.version++
	e.deleted = false
	e.data = clone(data)
	if meta != nil {
		e.meta = copyMeta(meta)
	}
}

// Delete soft-deletes the latest version.
func (k *MemKV) Delete(full string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if e := k.entries[full]; e != nil {
		e.deleted, e.data = true, nil
	}
}

// Get returns the stored state of a path.
func (k *MemKV) Get(full string) (data map[string]any, meta map[string]string, version int64, exists bool) {
	k.mu.Lock()
	defer k.mu.Unlock()
	e := k.entries[full]
	if e == nil {
		return nil, nil, 0, false
	}
	return clone(e.data), copyMeta(e.meta), e.version, !e.deleted
}

func copyMeta(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func (k *MemKV) KVRead(_ context.Context, mount, path string) (*vault.KVSecret, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.reads++
	if err := k.fail[key(mount, path)]; err != nil {
		return nil, err
	}
	e := k.entries[key(mount, path)]
	if e == nil {
		return &vault.KVSecret{}, nil
	}
	return &vault.KVSecret{Exists: !e.deleted, Version: e.version, Data: clone(e.data), Metadata: copyMeta(e.meta)}, nil
}

func (k *MemKV) KVWriteCAS(_ context.Context, mount, path string, data map[string]any, cas int64) (int64, error) {
	k.mu.Lock()
	hook := k.BeforeWrite
	k.BeforeWrite = nil
	k.mu.Unlock()
	if hook != nil {
		hook()
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	e := k.entries[key(mount, path)]
	current := int64(0)
	if e != nil {
		current = e.version
	}
	if cas != current {
		return 0, fmt.Errorf("write: %w", vault.ErrCASMismatch)
	}
	if e == nil {
		e = &entry{}
		k.entries[key(mount, path)] = e
	}
	e.version++
	e.deleted = false
	e.data = clone(data)
	k.dataWrites++
	return e.version, nil
}

func (k *MemKV) KVSetCustomMetadata(_ context.Context, mount, path string, md map[string]string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	e := k.entries[key(mount, path)]
	if e == nil {
		// The real policy has no create capability on metadata.
		return fmt.Errorf("write metadata: %w", vault.ErrForbidden)
	}
	e.meta = copyMeta(md)
	k.metaWrites++
	return nil
}

func (k *MemKV) KVDestroy(_ context.Context, mount, path string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.destroyErr != nil {
		return k.destroyErr
	}
	if _, ok := k.entries[key(mount, path)]; ok {
		delete(k.entries, key(mount, path))
		k.destroys++
	}
	return nil
}
