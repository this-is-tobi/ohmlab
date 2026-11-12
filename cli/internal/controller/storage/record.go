package storage

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/cli/internal/vault"
)

// KV is what the reconciler asks of Vault. *vault.Client implements it.
type KV interface {
	KVRead(ctx context.Context, mount, path string) (*vault.KVSecret, error)
	KVWriteCAS(ctx context.Context, mount, path string, data map[string]any, cas int64) (int64, error)
	KVSetCustomMetadata(ctx context.Context, mount, path string, md map[string]string) error
}

var _ KV = (*vault.Client)(nil)

// instrumentedKV counts and times every call to Vault, and counts the writes.
type instrumentedKV struct {
	inner KV
	m     *metrics.Metrics
}

func (i *instrumentedKV) observe(op string, start time.Time, err error) {
	result := "ok"
	switch {
	case errors.Is(err, vault.ErrCASMismatch):
		result = "conflict"
	case errors.Is(err, vault.ErrForbidden):
		result = "forbidden"
	case err != nil:
		result = "error"
	}
	i.m.BackendRequests.WithLabelValues(Target, op, result).Inc()
	i.m.BackendDuration.WithLabelValues(Target, op).Observe(time.Since(start).Seconds())
}

func (i *instrumentedKV) KVRead(ctx context.Context, mount, path string) (*vault.KVSecret, error) {
	start := time.Now()
	s, err := i.inner.KVRead(ctx, mount, path)
	i.observe("read", start, err)
	return s, err
}

func (i *instrumentedKV) KVWriteCAS(ctx context.Context, mount, path string, data map[string]any, cas int64) (int64, error) {
	start := time.Now()
	v, err := i.inner.KVWriteCAS(ctx, mount, path, data, cas)
	i.observe("write", start, err)
	if err == nil {
		i.m.BackendWrites.WithLabelValues(Target, kind, "data").Inc()
	}
	return v, err
}

func (i *instrumentedKV) KVSetCustomMetadata(ctx context.Context, mount, path string, md map[string]string) error {
	start := time.Now()
	err := i.inner.KVSetCustomMetadata(ctx, mount, path, md)
	i.observe("metadata", start, err)
	if err == nil {
		i.m.BackendWrites.WithLabelValues(Target, kind, "metadata").Inc()
	}
	return err
}

// Markers of a record, in its custom_metadata. ownerKey is the seed engine's
// owner key, so a VaultSeed aimed at a record stops at OwnedByAnother.
const (
	ownerKey       = "ohmlab.fr/owner"
	claimPrefix    = "ohmlab.fr/claim."
	tokenPrefix    = "ohmlab.fr/rotation-token."
	retiringPrefix = "ohmlab.fr/retiring."
)

const (
	fieldAccessKey = "accessKey"
	fieldSecretKey = "secretKey"
)

// entryFields are a claim's other fields, written from the spec and the store.
var entryFields = []string{"bucket", "ca", "endpoint", "pathStyle", "region"}

// claim is what one S3Bucket asks of its record.
type claim struct {
	owner  string            // <namespace>/<name> of the S3Bucket
	key    string            // the claim's key in the record
	fields map[string]string // the entryFields
	token  string            // spec.rotate.token; empty: none asked
}

// change is one planned action and what it touches, by name: a field, a
// claim, a bucket, a policy or a user. Never a value.
type change struct{ action, name string }

// recordPlan is what one claim needs of its record.
type recordPlan struct {
	data      map[string]any    // the record's data to write; nil: unchanged
	metadata  map[string]string // its custom_metadata to write; nil: unchanged
	changes   []change
	accessKey string // the claim's key once the plan is written
	secretKey string
	retiring  string // an older access key kept until its deadline
	retired   string // an older access key whose deadline passed with this plan
	blocked   string // why the claim may write nothing: another owner
}

// planRecord works out the record for one claim: keys generated once,
// rotated when the token is new, an older key kept until its deadline, the
// other fields from the spec, and the markers. Other claims' keys and markers
// are left as they are.
func planRecord(sec *vault.KVSecret, c claim, now time.Time, grace time.Duration, newKey func() (string, string, error)) (recordPlan, error) {
	md := sec.Metadata
	if owner := md[ownerKey]; owner != "" && owner != PathOwner {
		return recordPlan{blocked: fmt.Sprintf("the record is owned by %s", owner)}, nil
	}
	if by := md[claimPrefix+c.key]; by != "" && by != c.owner {
		return recordPlan{blocked: fmt.Sprintf("key %s of the record is claimed by %s", c.key, by)}, nil
	}

	var p recordPlan
	entry := map[string]any{}
	if e, ok := sec.Data[c.key].(map[string]any); ok {
		entry = maps.Clone(e)
	}
	meta := maps.Clone(md)
	if meta == nil {
		meta = map[string]string{}
	}
	dataChanged, metaChanged := false, false
	setKeys := func() error {
		ak, sk, err := newKey()
		if err != nil {
			return fmt.Errorf("generate a key: %w", err)
		}
		entry[fieldAccessKey], entry[fieldSecretKey] = ak, sk
		dataChanged = true
		return nil
	}

	ak, sk := str(entry[fieldAccessKey]), str(entry[fieldSecretKey])
	switch {
	case ak == "" || sk == "":
		if err := setKeys(); err != nil {
			return recordPlan{}, err
		}
		p.changes = append(p.changes, change{"add-key", fieldAccessKey}, change{"add-key", fieldSecretKey})
	case md[claimPrefix+c.key] == "":
		p.changes = append(p.changes, change{"adopt", c.key})
	}

	if c.token != "" && c.token != md[tokenPrefix+c.key] {
		if ak != "" && sk != "" {
			if err := setKeys(); err != nil {
				return recordPlan{}, err
			}
			meta[retiringPrefix+c.key] = ak + " " + now.Add(grace).UTC().Format(time.RFC3339)
			p.changes = append(p.changes, change{"rotate", c.key})
		}
		meta[tokenPrefix+c.key] = c.token
		metaChanged = true
	}
	p.accessKey, p.secretKey = str(entry[fieldAccessKey]), str(entry[fieldSecretKey])

	if r := meta[retiringPrefix+c.key]; r != "" {
		old, deadline, ok := parseRetiring(r)
		if ok && now.Before(deadline) && old != p.accessKey {
			p.retiring = old
		} else {
			delete(meta, retiringPrefix+c.key)
			metaChanged = true
			p.retired = old
			p.changes = append(p.changes, change{"retire-key", c.key})
		}
	}

	for _, f := range entryFields {
		if str(entry[f]) != c.fields[f] {
			entry[f] = c.fields[f]
			dataChanged = true
			p.changes = append(p.changes, change{"update-field", f})
		}
	}
	if meta[ownerKey] != PathOwner || meta[claimPrefix+c.key] != c.owner {
		meta[ownerKey], meta[claimPrefix+c.key] = PathOwner, c.owner
		metaChanged = true
	}

	if dataChanged {
		p.data = maps.Clone(sec.Data)
		if p.data == nil {
			p.data = map[string]any{}
		}
		p.data[c.key] = entry
	}
	if metaChanged {
		p.metadata = meta
	}
	return p, nil
}

// parseRetiring reads "<accessKey> <RFC 3339 deadline>".
func parseRetiring(s string) (string, time.Time, bool) {
	key, at, ok := strings.Cut(s, " ")
	if !ok {
		return key, time.Time{}, false
	}
	deadline, err := time.Parse(time.RFC3339, at)
	return key, deadline, err == nil && key != ""
}

// withoutClaim is the record once a claim's key and markers are removed.
func withoutClaim(sec *vault.KVSecret, key string) (map[string]any, map[string]string) {
	data := maps.Clone(sec.Data)
	if data == nil {
		data = map[string]any{}
	}
	delete(data, key)
	meta := maps.Clone(sec.Metadata)
	for _, prefix := range []string{claimPrefix, tokenPrefix, retiringPrefix} {
		delete(meta, prefix+key)
	}
	return data, meta
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
