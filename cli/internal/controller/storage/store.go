package storage

import (
	"context"
	"errors"
	"slices"
	"time"

	v1alpha1 "github.com/this-is-tobi/ohmlab/cli/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/cli/internal/controller/metrics"
	"github.com/this-is-tobi/ohmlab/cli/internal/rustfs"
)

// Store is what the reconciler asks of a store. *rustfs.Client implements it.
type Store interface {
	InspectBucket(ctx context.Context, name string) (rustfs.Bucket, error)
	CreateBucket(ctx context.Context, name string) error
	SetVersioning(ctx context.Context, name string, on bool) error
	SetLifecycle(ctx context.Context, name string, noncurrentDays, expireDays int) error
	SetQuota(ctx context.Context, name string, bytes uint64) error
	BucketEmpty(ctx context.Context, name string) (bool, error)
	RemoveBucket(ctx context.Context, name string) error
	Policy(ctx context.Context, name string) ([]byte, error)
	PutPolicy(ctx context.Context, name string, doc []byte) error
	RemovePolicy(ctx context.Context, name string) error
	User(ctx context.Context, name string) (rustfs.User, error)
	CreateUser(ctx context.Context, name, secret string) error
	AttachPolicy(ctx context.Context, policy, user string) error
	DetachPolicy(ctx context.Context, policy, user string) error
	RemoveUser(ctx context.Context, name string) error
	ServiceAccounts(ctx context.Context, user string) ([]string, error)
	AddServiceAccount(ctx context.Context, user, accessKey, secretKey string) error
	RemoveServiceAccount(ctx context.Context, accessKey string) error
}

var _ Store = (*rustfs.Client)(nil)

// instrumentedStore counts and times every call, and counts the writes: a
// write loop shows as a rate of ohmlab_backend_writes_total, as for Vault.
type instrumentedStore struct {
	inner Store
	m     *metrics.Metrics
}

func (s *instrumentedStore) observe(op string, start time.Time, err error, write bool) {
	result := "ok"
	switch {
	case errors.Is(err, rustfs.ErrForbidden):
		result = "forbidden"
	case errors.Is(err, rustfs.ErrNotFound):
		result = "notfound"
	case err != nil:
		result = "error"
	}
	s.m.BackendRequests.WithLabelValues(Target, op, result).Inc()
	s.m.BackendDuration.WithLabelValues(Target, op).Observe(time.Since(start).Seconds())
	if write && err == nil {
		s.m.BackendWrites.WithLabelValues(Target, kind, op).Inc()
	}
}

func (s *instrumentedStore) read(op string, f func() error) error {
	start := time.Now()
	err := f()
	s.observe(op, start, err, false)
	return err
}

func (s *instrumentedStore) write(op string, f func() error) error {
	start := time.Now()
	err := f()
	s.observe(op, start, err, true)
	return err
}

func (s *instrumentedStore) InspectBucket(ctx context.Context, name string) (b rustfs.Bucket, err error) {
	err = s.read("inspect-bucket", func() error { b, err = s.inner.InspectBucket(ctx, name); return err })
	return b, err
}

func (s *instrumentedStore) CreateBucket(ctx context.Context, name string) error {
	return s.write("create-bucket", func() error { return s.inner.CreateBucket(ctx, name) })
}

func (s *instrumentedStore) SetVersioning(ctx context.Context, name string, on bool) error {
	return s.write("set-versioning", func() error { return s.inner.SetVersioning(ctx, name, on) })
}

func (s *instrumentedStore) SetLifecycle(ctx context.Context, name string, noncurrentDays, expireDays int) error {
	return s.write("set-lifecycle", func() error { return s.inner.SetLifecycle(ctx, name, noncurrentDays, expireDays) })
}

func (s *instrumentedStore) SetQuota(ctx context.Context, name string, bytes uint64) error {
	return s.write("set-quota", func() error { return s.inner.SetQuota(ctx, name, bytes) })
}

func (s *instrumentedStore) BucketEmpty(ctx context.Context, name string) (empty bool, err error) {
	err = s.read("bucket-empty", func() error { empty, err = s.inner.BucketEmpty(ctx, name); return err })
	return empty, err
}

func (s *instrumentedStore) RemoveBucket(ctx context.Context, name string) error {
	return s.write("remove-bucket", func() error { return s.inner.RemoveBucket(ctx, name) })
}

func (s *instrumentedStore) Policy(ctx context.Context, name string) (doc []byte, err error) {
	err = s.read("policy", func() error { doc, err = s.inner.Policy(ctx, name); return err })
	return doc, err
}

func (s *instrumentedStore) PutPolicy(ctx context.Context, name string, doc []byte) error {
	return s.write("put-policy", func() error { return s.inner.PutPolicy(ctx, name, doc) })
}

func (s *instrumentedStore) RemovePolicy(ctx context.Context, name string) error {
	return s.write("remove-policy", func() error { return s.inner.RemovePolicy(ctx, name) })
}

func (s *instrumentedStore) User(ctx context.Context, name string) (u rustfs.User, err error) {
	err = s.read("user", func() error { u, err = s.inner.User(ctx, name); return err })
	return u, err
}

func (s *instrumentedStore) CreateUser(ctx context.Context, name, secret string) error {
	return s.write("create-user", func() error { return s.inner.CreateUser(ctx, name, secret) })
}

func (s *instrumentedStore) AttachPolicy(ctx context.Context, policy, user string) error {
	return s.write("attach-policy", func() error { return s.inner.AttachPolicy(ctx, policy, user) })
}

func (s *instrumentedStore) DetachPolicy(ctx context.Context, policy, user string) error {
	return s.write("detach-policy", func() error { return s.inner.DetachPolicy(ctx, policy, user) })
}

func (s *instrumentedStore) RemoveUser(ctx context.Context, name string) error {
	return s.write("remove-user", func() error { return s.inner.RemoveUser(ctx, name) })
}

func (s *instrumentedStore) ServiceAccounts(ctx context.Context, user string) (keys []string, err error) {
	err = s.read("service-accounts", func() error { keys, err = s.inner.ServiceAccounts(ctx, user); return err })
	return keys, err
}

func (s *instrumentedStore) AddServiceAccount(ctx context.Context, user, accessKey, secretKey string) error {
	return s.write("add-service-account", func() error { return s.inner.AddServiceAccount(ctx, user, accessKey, secretKey) })
}

func (s *instrumentedStore) RemoveServiceAccount(ctx context.Context, accessKey string) error {
	return s.write("remove-service-account", func() error { return s.inner.RemoveServiceAccount(ctx, accessKey) })
}

// storeWant is what one claim needs of its store.
type storeWant struct {
	bucket, user, policy       string
	doc                        []byte
	versioning                 bool
	noncurrentDays, expireDays int
	quota                      uint64
}

func wantOf(obj *v1alpha1.S3Bucket) (storeWant, error) {
	access := obj.Spec.Access
	if access == "" {
		access = v1alpha1.S3AccessReadWrite
	}
	doc, err := rustfs.BucketPolicy(obj.Spec.Bucket, string(access))
	if err != nil {
		return storeWant{}, err
	}
	noncurrent := int(obj.Spec.Lifecycle.NoncurrentDays)
	if noncurrent <= 0 {
		noncurrent = 7
	}
	return storeWant{
		bucket: obj.Spec.Bucket, user: obj.Spec.Bucket, policy: "ohmlab-" + obj.Spec.Bucket + "-" + string(access), doc: doc,
		versioning:     obj.Spec.Versioning == nil || *obj.Spec.Versioning,
		noncurrentDays: noncurrent, expireDays: int(obj.Spec.Lifecycle.ExpireDays),
		quota: uint64(obj.Spec.Quota.Value()),
	}, nil
}

// storeState is what a store holds of one claim.
type storeState struct {
	bucket rustfs.Bucket
	policy []byte
	user   rustfs.User
	keys   []string
}

func observeStore(ctx context.Context, s Store, w storeWant) (storeState, error) {
	var st storeState
	var err error
	if st.bucket, err = s.InspectBucket(ctx, w.bucket); err != nil {
		return st, err
	}
	if st.policy, err = s.Policy(ctx, w.policy); err != nil {
		return st, err
	}
	if st.user, err = s.User(ctx, w.user); err != nil {
		return st, err
	}
	if st.user.Exists {
		if st.keys, err = s.ServiceAccounts(ctx, w.user); err != nil {
			return st, err
		}
	}
	return st, nil
}

// storeAction is one write to a store. name is the bucket, policy or user it
// acts on; a key never reaches a message whole.
type storeAction struct {
	op, name string
	key      string // the access key a remove-service-account removes
	stray    bool   // a key the record never named
	do       func(ctx context.Context, s Store) error
}

// planStore lists the writes that make the store match the record, in order:
// the bucket and its settings, the policy, the user and its policies, then
// the keys: the record's is added, any other but a key in its grace removed.
func planStore(st storeState, w storeWant, rp recordPlan) []storeAction {
	var out []storeAction
	add := func(a storeAction) { out = append(out, a) }
	b := st.bucket
	if !b.Exists {
		add(storeAction{op: "create-bucket", name: w.bucket, do: func(ctx context.Context, s Store) error { return s.CreateBucket(ctx, w.bucket) }})
	}
	if b.Versioning != w.versioning {
		add(storeAction{op: "set-versioning", name: w.bucket, do: func(ctx context.Context, s Store) error { return s.SetVersioning(ctx, w.bucket, w.versioning) }})
	}
	if b.NoncurrentDays != w.noncurrentDays || b.ExpireDays != w.expireDays {
		add(storeAction{op: "set-lifecycle", name: w.bucket, do: func(ctx context.Context, s Store) error {
			return s.SetLifecycle(ctx, w.bucket, w.noncurrentDays, w.expireDays)
		}})
	}
	if b.QuotaBytes != w.quota {
		add(storeAction{op: "set-quota", name: w.bucket, do: func(ctx context.Context, s Store) error { return s.SetQuota(ctx, w.bucket, w.quota) }})
	}
	if st.policy == nil || !rustfs.SamePolicy(st.policy, w.doc) {
		add(storeAction{op: "put-policy", name: w.policy, do: func(ctx context.Context, s Store) error { return s.PutPolicy(ctx, w.policy, w.doc) }})
	}
	if !st.user.Exists {
		// The user's own secret is never stored: nothing logs in as the user.
		add(storeAction{op: "create-user", name: w.user, do: func(ctx context.Context, s Store) error {
			secret, err := randomString(secretKeyLength, secretKeyAlphabet)
			if err != nil {
				return err
			}
			return s.CreateUser(ctx, w.user, secret)
		}})
	}
	if !slices.Contains(st.user.Policies, w.policy) {
		add(storeAction{op: "attach-policy", name: w.policy, do: func(ctx context.Context, s Store) error { return s.AttachPolicy(ctx, w.policy, w.user) }})
	}
	for _, p := range st.user.Policies {
		if p != w.policy {
			add(storeAction{op: "detach-policy", name: p, do: func(ctx context.Context, s Store) error { return s.DetachPolicy(ctx, p, w.user) }})
		}
	}
	if !slices.Contains(st.keys, rp.accessKey) {
		add(storeAction{op: "add-service-account", name: w.user, do: func(ctx context.Context, s Store) error {
			return s.AddServiceAccount(ctx, w.user, rp.accessKey, rp.secretKey)
		}})
	}
	for _, k := range st.keys {
		if k == rp.accessKey || k == rp.retiring {
			continue
		}
		add(storeAction{op: "remove-service-account", name: w.user, key: k, stray: k != rp.retired,
			do: func(ctx context.Context, s Store) error { return s.RemoveServiceAccount(ctx, k) }})
	}
	return out
}
