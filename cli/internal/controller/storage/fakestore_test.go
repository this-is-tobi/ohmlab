package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/this-is-tobi/ohmlab/cli/internal/rustfs"
)

// fakeStore is an in-memory RustFS with the behaviour the target relies on,
// as internal/rustfs's integration tests found it on a real store: a policy
// reads back in another layout, an attached policy cannot be removed, a
// bucket holding a version cannot be removed, a missing object is removed
// already.
type fakeStore struct {
	mu       sync.Mutex
	buckets  map[string]*fakeBucket
	policies map[string][]byte
	users    map[string]*fakeUser
	keys     map[string]fakeKey // service accounts, by access key
	writes   map[string]int
	ops      []string // every write, in order: "<op> <name>"
	fail     map[string]failure
}

type failure struct {
	err    error
	sticky bool // until heal, not once
}

type fakeBucket struct {
	versioning                 bool
	noncurrentDays, expireDays int
	quota                      uint64
	objects                    int
}

type fakeUser struct {
	secret   string
	policies []string
}

type fakeKey struct{ user, secret string }

var errFake = errors.New("connection refused")

func newFakeStore() *fakeStore {
	f := &fakeStore{fail: map[string]failure{}}
	f.wipe()
	return f
}

// wipe empties the store, as a rebuild on an empty volume does.
func (f *fakeStore) wipe() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buckets, f.policies, f.users, f.keys = map[string]*fakeBucket{}, map[string][]byte{}, map[string]*fakeUser{}, map[string]fakeKey{}
	f.writes, f.ops = map[string]int{}, nil
}

// failNext makes the next call of op fail with err.
func (f *fakeStore) failNext(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[op] = failure{err: err}
}

// failUntilHealed makes every call of op fail with err until heal.
func (f *fakeStore) failUntilHealed(op string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail[op] = failure{err: err, sticky: true}
}

func (f *fakeStore) heal(op string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.fail, op)
}

func (f *fakeStore) take(op string) error {
	fl, ok := f.fail[op]
	if !ok {
		return nil
	}
	if !fl.sticky {
		delete(f.fail, op)
	}
	return fl.err
}

func (f *fakeStore) wrote(op, name string) {
	f.writes[op]++
	f.ops = append(f.ops, op+" "+name)
}

func (f *fakeStore) writeCount(op string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.writes[op]
}

func (f *fakeStore) totalWrites() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.writes {
		n += c
	}
	return n
}

func (f *fakeStore) opsLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.ops)
}

func (f *fakeStore) putObject(bucket string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.buckets[bucket].objects++
}

func (f *fakeStore) bucket(name string) (fakeBucket, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.buckets[name]
	if b == nil {
		return fakeBucket{}, false
	}
	return *b, true
}

func (f *fakeStore) user(name string) (fakeUser, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u := f.users[name]
	if u == nil {
		return fakeUser{}, false
	}
	return fakeUser{secret: u.secret, policies: slices.Clone(u.policies)}, true
}

// policy is the document as it was put.
func (f *fakeStore) policy(name string) []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.policies[name]
}

// keyOf reports the user and secret of a service account.
func (f *fakeStore) keyOf(accessKey string) (fakeKey, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k, ok := f.keys[accessKey]
	return k, ok
}

// addKey adds a service account behind the controller's back.
func (f *fakeStore) addKey(user, accessKey, secretKey string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.keys[accessKey] = fakeKey{user: user, secret: secretKey}
}

// attach attaches a policy behind the controller's back.
func (f *fakeStore) attach(policy, user string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.policies[policy] = []byte(`{"Version":"2012-10-17","Statement":[]}`)
	f.users[user].policies = append(f.users[user].policies, policy)
}

func (f *fakeStore) keysOf(user string) []string {
	var out []string
	for ak, k := range f.keys {
		if k.user == user {
			out = append(out, ak)
		}
	}
	slices.Sort(out)
	return out
}

func (f *fakeStore) InspectBucket(_ context.Context, name string) (rustfs.Bucket, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("inspect-bucket"); err != nil {
		return rustfs.Bucket{}, err
	}
	b := f.buckets[name]
	if b == nil {
		return rustfs.Bucket{}, nil
	}
	return rustfs.Bucket{Exists: true, Versioning: b.versioning, NoncurrentDays: b.noncurrentDays, ExpireDays: b.expireDays, QuotaBytes: b.quota}, nil
}

func (f *fakeStore) CreateBucket(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("create-bucket"); err != nil {
		return err
	}
	if f.buckets[name] == nil {
		f.buckets[name] = &fakeBucket{}
	}
	f.wrote("create-bucket", name)
	return nil
}

func (f *fakeStore) withBucket(op, name string, set func(*fakeBucket)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take(op); err != nil {
		return err
	}
	b := f.buckets[name]
	if b == nil {
		return fmt.Errorf("%s %s: %w", op, name, rustfs.ErrNotFound)
	}
	set(b)
	f.wrote(op, name)
	return nil
}

func (f *fakeStore) SetVersioning(_ context.Context, name string, on bool) error {
	return f.withBucket("set-versioning", name, func(b *fakeBucket) { b.versioning = on })
}

func (f *fakeStore) SetLifecycle(_ context.Context, name string, noncurrentDays, expireDays int) error {
	return f.withBucket("set-lifecycle", name, func(b *fakeBucket) { b.noncurrentDays, b.expireDays = noncurrentDays, expireDays })
}

func (f *fakeStore) SetQuota(_ context.Context, name string, bytes uint64) error {
	return f.withBucket("set-quota", name, func(b *fakeBucket) { b.quota = bytes })
}

func (f *fakeStore) BucketEmpty(_ context.Context, name string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("bucket-empty"); err != nil {
		return false, err
	}
	b := f.buckets[name]
	return b == nil || b.objects == 0, nil
}

func (f *fakeStore) RemoveBucket(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("remove-bucket"); err != nil {
		return err
	}
	if b := f.buckets[name]; b != nil && b.objects > 0 {
		return errors.New("remove-bucket: BucketNotEmpty")
	}
	delete(f.buckets, name)
	f.wrote("remove-bucket", name)
	return nil
}

// Policy answers in another layout than the document put, as RustFS does.
func (f *fakeStore) Policy(_ context.Context, name string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("policy"); err != nil {
		return nil, err
	}
	doc := f.policies[name]
	if doc == nil {
		return nil, nil
	}
	var v any
	if err := json.Unmarshal(doc, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

func (f *fakeStore) PutPolicy(_ context.Context, name string, doc []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("put-policy"); err != nil {
		return err
	}
	f.policies[name] = slices.Clone(doc)
	f.wrote("put-policy", name)
	return nil
}

func (f *fakeStore) RemovePolicy(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("remove-policy"); err != nil {
		return err
	}
	for _, u := range f.users {
		if slices.Contains(u.policies, name) {
			return errors.New("remove-policy: policy in use")
		}
	}
	delete(f.policies, name)
	f.wrote("remove-policy", name)
	return nil
}

func (f *fakeStore) User(_ context.Context, name string) (rustfs.User, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("user"); err != nil {
		return rustfs.User{}, err
	}
	u := f.users[name]
	if u == nil {
		return rustfs.User{}, nil
	}
	p := slices.Clone(u.policies)
	slices.Sort(p)
	return rustfs.User{Exists: true, Policies: p}, nil
}

func (f *fakeStore) CreateUser(_ context.Context, name, secret string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("create-user"); err != nil {
		return err
	}
	if u := f.users[name]; u != nil {
		u.secret = secret
	} else {
		f.users[name] = &fakeUser{secret: secret}
	}
	f.wrote("create-user", name)
	return nil
}

func (f *fakeStore) AttachPolicy(_ context.Context, policy, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("attach-policy"); err != nil {
		return err
	}
	u := f.users[user]
	if u == nil || f.policies[policy] == nil {
		return errors.New("attach-policy: InvalidArgument: user or policy not found")
	}
	if !slices.Contains(u.policies, policy) {
		u.policies = append(u.policies, policy)
	}
	f.wrote("attach-policy", policy)
	return nil
}

func (f *fakeStore) DetachPolicy(_ context.Context, policy, user string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("detach-policy"); err != nil {
		return err
	}
	if u := f.users[user]; u != nil {
		u.policies = slices.DeleteFunc(u.policies, func(p string) bool { return p == policy })
	}
	f.wrote("detach-policy", policy)
	return nil
}

func (f *fakeStore) RemoveUser(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("remove-user"); err != nil {
		return err
	}
	delete(f.users, name)
	f.wrote("remove-user", name)
	return nil
}

func (f *fakeStore) ServiceAccounts(_ context.Context, user string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("service-accounts"); err != nil {
		return nil, err
	}
	return f.keysOf(user), nil
}

func (f *fakeStore) AddServiceAccount(_ context.Context, user, accessKey, secretKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("add-service-account"); err != nil {
		return err
	}
	if f.users[user] == nil {
		return errors.New("add-service-account: NoSuchResource: user does not exist")
	}
	if _, ok := f.keys[accessKey]; ok {
		return errors.New("add-service-account: InvalidArgument: access key is already in use")
	}
	f.keys[accessKey] = fakeKey{user: user, secret: secretKey}
	f.wrote("add-service-account", user)
	return nil
}

func (f *fakeStore) RemoveServiceAccount(_ context.Context, accessKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.take("remove-service-account"); err != nil {
		return err
	}
	k := f.keys[accessKey]
	delete(f.keys, accessKey)
	f.wrote("remove-service-account", k.user+" "+accessKey)
	return nil
}
