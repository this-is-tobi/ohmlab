package rustfs

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

// The integration tests run against a real RustFS store:
//   - OHMLAB_RUSTFS_ENDPOINT: host:port
//   - OHMLAB_RUSTFS_CA: the store's CA, PEM
//   - OHMLAB_RUSTFS_ROOT_FILE: an env file with RUSTFS_ROOT_USER and RUSTFS_ROOT_PASSWORD
//   - OHMLAB_RUSTFS_REGION: the store's region, us-east-1 when unset
//
// Without them they skip, unless OHMLAB_REQUIRE_RUSTFS is set. Every test
// makes names of its own and removes what it made.

func storeConfig(t *testing.T) Config {
	t.Helper()
	endpoint, ca, rootFile := os.Getenv("OHMLAB_RUSTFS_ENDPOINT"), os.Getenv("OHMLAB_RUSTFS_CA"), os.Getenv("OHMLAB_RUSTFS_ROOT_FILE")
	if endpoint == "" || ca == "" || rootFile == "" {
		if os.Getenv("OHMLAB_REQUIRE_RUSTFS") != "" {
			t.Fatal("OHMLAB_REQUIRE_RUSTFS is set: OHMLAB_RUSTFS_ENDPOINT, OHMLAB_RUSTFS_CA and OHMLAB_RUSTFS_ROOT_FILE are required")
		}
		t.Skip("no RustFS store: set OHMLAB_RUSTFS_ENDPOINT, OHMLAB_RUSTFS_CA and OHMLAB_RUSTFS_ROOT_FILE")
	}
	region := os.Getenv("OHMLAB_RUSTFS_REGION")
	if region == "" {
		region = "us-east-1"
	}
	user, password := readRoot(t, rootFile)
	return Config{Endpoint: endpoint, CAFile: ca, Region: region, AccessKey: user, SecretKey: password}
}

// readRoot reads the root keys from an env file, never printing them.
func readRoot(t *testing.T, file string) (user, password string) {
	t.Helper()
	f, err := os.Open(file)
	if err != nil {
		t.Fatalf("root keys: %v", err)
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, v, _ := strings.Cut(s.Text(), "=")
		switch k {
		case "RUSTFS_ROOT_USER":
			user = v
		case "RUSTFS_ROOT_PASSWORD":
			password = v
		}
	}
	if user == "" || password == "" {
		t.Fatal("root keys: RUSTFS_ROOT_USER or RUSTFS_ROOT_PASSWORD missing")
	}
	return user, password
}

func rootClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(storeConfig(t))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

// keyClient is a client holding another identity's key.
func keyClient(t *testing.T, accessKey, secretKey string) *Client {
	t.Helper()
	cfg := storeConfig(t)
	cfg.AccessKey, cfg.SecretKey = accessKey, secretKey
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func randName(prefix string) string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return prefix + "-" + hex.EncodeToString(b)
}

// randKeys returns a service account pair shaped like the storage target's:
// 20 characters [A-Z0-9], 40 characters [a-z0-9].
func randKeys() (string, string) {
	a, s := make([]byte, 10), make([]byte, 20)
	_, _ = rand.Read(a)
	_, _ = rand.Read(s)
	return strings.ToUpper(hex.EncodeToString(a)), hex.EncodeToString(s)
}

func put(c *Client, t *testing.T, bucket, key string, size int) error {
	t.Helper()
	data := make([]byte, size)
	_, _ = rand.Read(data)
	_, err := c.s3.PutObject(t.Context(), bucket, key, bytes.NewReader(data), int64(size), minio.PutObjectOptions{})
	return err
}

// eventually retries f until it returns nil or timeout passes: a policy
// reaches the S3 path a moment after the admin call returns.
func eventually(timeout time.Duration, f func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		err := f()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// emptyBucket removes every version and delete marker, as root.
func emptyBucket(t *testing.T, root *Client, name string) {
	t.Helper()
	ctx := context.Background()
	for o := range root.s3.ListObjects(ctx, name, minio.ListObjectsOptions{Recursive: true, WithVersions: true}) {
		if o.Err == nil {
			_ = root.s3.RemoveObject(ctx, name, o.Key, minio.RemoveObjectOptions{VersionID: o.VersionID})
		}
	}
}

// Cleanups run last-registered first, and RustFS refuses to remove an
// attached policy: register a policy's cleanup before its user's, and a
// user's before its service accounts'.

func makeBucket(t *testing.T, root *Client, name string) {
	t.Helper()
	if err := root.CreateBucket(t.Context(), name); err != nil {
		t.Fatalf("CreateBucket %s: %v", name, err)
	}
	t.Cleanup(func() {
		emptyBucket(t, root, name)
		if err := root.RemoveBucket(context.Background(), name); err != nil {
			t.Logf("cleanup: RemoveBucket %s: %v", name, err)
		}
	})
}

func removePolicyAtCleanup(t *testing.T, root *Client, name string) {
	t.Cleanup(func() {
		if err := root.RemovePolicy(context.Background(), name); err != nil {
			t.Logf("cleanup: RemovePolicy %s: %v", name, err)
		}
	})
}

func removeUserAtCleanup(t *testing.T, root *Client, name string) {
	t.Cleanup(func() {
		if err := root.RemoveUser(context.Background(), name); err != nil {
			t.Logf("cleanup: RemoveUser %s: %v", name, err)
		}
	})
}

func removeServiceAccountAtCleanup(t *testing.T, root *Client, accessKey string) {
	t.Cleanup(func() {
		if err := root.RemoveServiceAccount(context.Background(), accessKey); err != nil {
			t.Logf("cleanup: RemoveServiceAccount: %v", err)
		}
	})
}

// confine gives bucket an identity as the storage target does: a user named
// after it, its canned policy, and one service account, whose keys it returns.
func confine(t *testing.T, root *Client, bucket string) (string, string) {
	t.Helper()
	ctx := t.Context()
	policy := "ohmlab-" + bucket + "-readWrite"
	doc, err := BucketPolicy(bucket, "readWrite")
	if err != nil {
		t.Fatal(err)
	}
	removePolicyAtCleanup(t, root, policy)
	if err := root.PutPolicy(ctx, policy, doc); err != nil {
		t.Fatalf("PutPolicy: %v", err)
	}
	removeUserAtCleanup(t, root, bucket)
	if err := root.CreateUser(ctx, bucket, hex.EncodeToString([]byte(randName("secret")))); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := root.AttachPolicy(ctx, policy, bucket); err != nil {
		t.Fatalf("AttachPolicy: %v", err)
	}
	ak, sk := randKeys()
	removeServiceAccountAtCleanup(t, root, ak)
	if err := root.AddServiceAccount(ctx, bucket, ak, sk); err != nil {
		t.Fatalf("AddServiceAccount: %v", err)
	}
	return ak, sk
}

func TestBucketRoundTrip(t *testing.T) {
	c, ctx := rootClient(t), t.Context()
	name := randName("it-bkt")
	if b, err := c.InspectBucket(ctx, name); err != nil || b.Exists {
		t.Fatalf("before creation: %+v, %v", b, err)
	}
	makeBucket(t, c, name)
	if err := c.SetVersioning(ctx, name, true); err != nil {
		t.Fatalf("SetVersioning: %v", err)
	}
	if err := c.SetLifecycle(ctx, name, 7, 0); err != nil {
		t.Fatalf("SetLifecycle(7, 0): %v", err)
	}
	if err := c.SetQuota(ctx, name, 8<<20); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	want := Bucket{Exists: true, Versioning: true, NoncurrentDays: 7, QuotaBytes: 8 << 20}
	if got, err := c.InspectBucket(ctx, name); err != nil || got != want {
		t.Errorf("InspectBucket = %+v, %v; want %+v", got, err, want)
	}
	if err := c.SetLifecycle(ctx, name, 7, 60); err != nil {
		t.Fatalf("SetLifecycle(7, 60): %v", err)
	}
	want.ExpireDays = 60
	if got, err := c.InspectBucket(ctx, name); err != nil || got != want {
		t.Errorf("InspectBucket after (7, 60) = %+v, %v; want %+v", got, err, want)
	}

	if empty, err := c.BucketEmpty(ctx, name); err != nil || !empty {
		t.Errorf("BucketEmpty of a new bucket = %v, %v", empty, err)
	}
	if err := put(c, t, name, "o", 1); err != nil {
		t.Fatal(err)
	}
	if empty, err := c.BucketEmpty(ctx, name); err != nil || empty {
		t.Errorf("BucketEmpty with one object = %v, %v", empty, err)
	}
	// A plain delete leaves the version and a delete marker: still not empty.
	if err := c.s3.RemoveObject(ctx, name, "o", minio.RemoveObjectOptions{}); err != nil {
		t.Fatal(err)
	}
	if empty, err := c.BucketEmpty(ctx, name); err != nil || empty {
		t.Errorf("BucketEmpty with a noncurrent version = %v, %v", empty, err)
	}
	emptyBucket(t, c, name)
	if empty, err := c.BucketEmpty(ctx, name); err != nil || !empty {
		t.Errorf("BucketEmpty once emptied = %v, %v", empty, err)
	}
	if err := c.RemoveBucket(ctx, name); err != nil {
		t.Fatalf("RemoveBucket: %v", err)
	}
	if b, err := c.InspectBucket(ctx, name); err != nil || b.Exists {
		t.Errorf("after removal: %+v, %v", b, err)
	}
}

func TestIdentityRoundTrip(t *testing.T) {
	c, ctx := rootClient(t), t.Context()
	user := randName("it-usr")
	policy := "ohmlab-" + user + "-readWrite"
	doc, err := BucketPolicy(user, "readWrite")
	if err != nil {
		t.Fatal(err)
	}

	removePolicyAtCleanup(t, c, policy)
	if got, err := c.Policy(ctx, policy); err != nil || got != nil {
		t.Fatalf("Policy before PutPolicy = %q, %v; want nil, nil", got, err)
	}
	if err := c.PutPolicy(ctx, policy, doc); err != nil {
		t.Fatalf("PutPolicy: %v", err)
	}
	if got, err := c.Policy(ctx, policy); err != nil || !SamePolicy(got, doc) {
		t.Errorf("Policy read back = %s, %v; want the document put", got, err)
	}

	removeUserAtCleanup(t, c, user)
	if u, err := c.User(ctx, user); err != nil || u.Exists {
		t.Fatalf("User before CreateUser = %+v, %v", u, err)
	}
	if err := c.CreateUser(ctx, user, hex.EncodeToString([]byte(randName("secret")))); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	for i := range 2 {
		if err := c.AttachPolicy(ctx, policy, user); err != nil {
			t.Fatalf("AttachPolicy #%d: %v", i+1, err)
		}
	}
	if u, err := c.User(ctx, user); err != nil || !u.Exists || !slices.Equal(u.Policies, []string{policy}) {
		t.Errorf("User = %+v, %v; want it with policy %s", u, err, policy)
	}

	k1, s1 := randKeys()
	k2, s2 := randKeys()
	removeServiceAccountAtCleanup(t, c, k1)
	removeServiceAccountAtCleanup(t, c, k2)
	if err := c.AddServiceAccount(ctx, user, k1, s1); err != nil {
		t.Fatalf("AddServiceAccount k1: %v", err)
	}
	if err := c.AddServiceAccount(ctx, user, k2, s2); err != nil {
		t.Fatalf("AddServiceAccount k2: %v", err)
	}
	sorted := []string{k1, k2}
	slices.Sort(sorted)
	if got, err := c.ServiceAccounts(ctx, user); err != nil || !slices.Equal(got, sorted) {
		t.Errorf("ServiceAccounts = %d keys, %v; want 2, sorted", len(got), err)
	}
	if err := c.RemoveServiceAccount(ctx, k1); err != nil {
		t.Fatalf("RemoveServiceAccount k1: %v", err)
	}
	if got, err := c.ServiceAccounts(ctx, user); err != nil || !slices.Equal(got, []string{k2}) {
		t.Errorf("ServiceAccounts after removing k1 = %d keys, %v; want k2 only", len(got), err)
	}

	if err := c.DetachPolicy(ctx, policy, user); err != nil {
		t.Fatalf("DetachPolicy: %v", err)
	}
	if u, err := c.User(ctx, user); err != nil || len(u.Policies) != 0 {
		t.Errorf("User after DetachPolicy = %+v, %v; want no policy", u, err)
	}

	if err := c.RemoveServiceAccount(ctx, k2); err != nil {
		t.Fatalf("RemoveServiceAccount k2: %v", err)
	}
	if err := c.RemoveUser(ctx, user); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}
	if u, err := c.User(ctx, user); err != nil || u.Exists {
		t.Errorf("User after RemoveUser = %+v, %v", u, err)
	}
	if err := c.RemovePolicy(ctx, policy); err != nil {
		t.Fatalf("RemovePolicy: %v", err)
	}
	if got, err := c.Policy(ctx, policy); err != nil || got != nil {
		t.Errorf("Policy after RemovePolicy = %q, %v", got, err)
	}
	// Removing what is gone is done: a deletion that stopped half-way resumes.
	for name, err := range map[string]error{
		"RemoveServiceAccount": c.RemoveServiceAccount(ctx, k1),
		"RemoveUser":           c.RemoveUser(ctx, user),
		"RemovePolicy":         c.RemovePolicy(ctx, policy),
	} {
		if err != nil {
			t.Errorf("%s again: %v", name, err)
		}
	}
}

func forbidden(op string, err error) bool { return errors.Is(wrapErr(op, err), ErrForbidden) }

func TestAServiceAccountKeyIsConfinedToItsBucket(t *testing.T) {
	root, ctx := rootClient(t), t.Context()
	a, b := randName("it-isa"), randName("it-isb")
	makeBucket(t, root, a)
	makeBucket(t, root, b)
	if err := root.SetVersioning(ctx, a, true); err != nil {
		t.Fatal(err)
	}
	if err := put(root, t, b, "root-object", 1); err != nil {
		t.Fatal(err)
	}
	ak, sk := confine(t, root, a)
	app := keyClient(t, ak, sk)
	if err := eventually(5*time.Second, func() error { return put(app, t, a, "o", 1) }); err != nil {
		t.Fatalf("put in A: %v", err)
	}

	o, err := app.s3.GetObject(ctx, a, "o", minio.GetObjectOptions{})
	if err == nil {
		_, err = io.ReadAll(o)
	}
	if err != nil {
		t.Errorf("get in A: %v", err)
	}
	for o := range app.s3.ListObjects(ctx, a, minio.ListObjectsOptions{}) {
		if o.Err != nil {
			t.Errorf("list in A: %v", o.Err)
		}
	}
	if err := put(app, t, a, "o", 1); err != nil {
		t.Errorf("second put in A: %v", err)
	}
	var first string
	for o := range root.s3.ListObjects(ctx, a, minio.ListObjectsOptions{Prefix: "o", WithVersions: true}) {
		if o.Err == nil && o.Key == "o" && !o.IsLatest {
			first = o.VersionID
		}
	}
	if err := app.s3.RemoveObject(ctx, a, "o", minio.RemoveObjectOptions{}); err != nil {
		t.Errorf("delete in A: %v", err)
	}
	if first == "" {
		t.Fatal("no noncurrent version of o in A")
	}
	if err := app.s3.RemoveObject(ctx, a, "o", minio.RemoveObjectOptions{VersionID: first}); !forbidden("delete-version", err) {
		t.Errorf("deleting a version in A: want ErrForbidden, got %v", err)
	}

	if err := put(app, t, b, "o", 1); !forbidden("put", err) {
		t.Errorf("put in B: want ErrForbidden, got %v", err)
	}
	o, err = app.s3.GetObject(ctx, b, "root-object", minio.GetObjectOptions{})
	if err == nil {
		_, err = io.ReadAll(o)
	}
	if !forbidden("get", err) {
		t.Errorf("get in B: want ErrForbidden, got %v", err)
	}
	var listErr error
	for o := range app.s3.ListObjects(ctx, b, minio.ListObjectsOptions{}) {
		if o.Err != nil {
			listErr = o.Err
		}
	}
	if !forbidden("list", listErr) {
		t.Errorf("list in B: want ErrForbidden, got %v", listErr)
	}
	if err := app.s3.RemoveObject(ctx, b, "root-object", minio.RemoveObjectOptions{}); !forbidden("delete", err) {
		t.Errorf("delete in B: want ErrForbidden, got %v", err)
	}

	if err := app.SetVersioning(ctx, a, false); !errors.Is(err, ErrForbidden) {
		t.Errorf("suspending versioning: want ErrForbidden, got %v", err)
	}
	if err := app.SetLifecycle(ctx, a, 1, 0); !errors.Is(err, ErrForbidden) {
		t.Errorf("setting lifecycle: want ErrForbidden, got %v", err)
	}
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"AWS":["*"]},"Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/*"]}]}`, a)
	if err := app.s3.SetBucketPolicy(ctx, a, policy); !forbidden("set-bucket-policy", err) {
		t.Errorf("setting a bucket policy: want ErrForbidden, got %v", err)
	}
}

func isQuotaRefusal(err error) bool {
	r := minio.ToErrorResponse(err)
	text := strings.ToLower(r.Code + " " + r.Message)
	return r.StatusCode >= 400 && r.StatusCode < 500 && strings.Contains(text, "quota")
}

func TestQuotaRefusesBeyondLimit(t *testing.T) {
	root, ctx := rootClient(t), t.Context()
	name := randName("it-q")
	makeBucket(t, root, name)
	if err := root.SetQuota(ctx, name, 8<<20); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}
	ak, sk := confine(t, root, name)
	app := keyClient(t, ak, sk)
	if err := eventually(5*time.Second, func() error { return put(app, t, name, "probe", 1) }); err != nil {
		t.Fatalf("policy never took effect: %v", err)
	}
	for i := range 7 {
		if err := put(app, t, name, fmt.Sprintf("mib-%d", i), 1<<20); err != nil {
			t.Fatalf("write %d of 7 MiB: %v", i+1, err)
		}
	}
	if err := put(app, t, name, "crossing", 2<<20); !isQuotaRefusal(err) {
		t.Errorf("the write crossing 8 MiB: want a quota refusal, got %v", err)
	}
}
