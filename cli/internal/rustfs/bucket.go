package rustfs

import (
	"context"
	"errors"

	"github.com/minio/madmin-go/v4"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

// LifecycleRule is the ID of the one lifecycle rule the storage target sets.
const LifecycleRule = "ohmlab"

// Bucket is what InspectBucket reads of a bucket.
type Bucket struct {
	Exists, Versioning bool
	// NoncurrentDays and ExpireDays are rule LifecycleRule's, 0 when absent.
	NoncurrentDays, ExpireDays int
	QuotaBytes                 uint64
}

// InspectBucket reads a bucket's versioning, lifecycle rule and quota. A
// missing bucket is Exists false, with no error.
func (c *Client) InspectBucket(ctx context.Context, name string) (Bucket, error) {
	exists, err := c.s3.BucketExists(ctx, name)
	if err != nil || !exists {
		return Bucket{}, wrapErr("inspect-bucket "+name, err)
	}
	b := Bucket{Exists: true}
	v, err := c.s3.GetBucketVersioning(ctx, name)
	if err != nil {
		return Bucket{}, wrapErr("inspect-bucket "+name, err)
	}
	b.Versioning = v.Enabled()
	lc, err := c.s3.GetBucketLifecycle(ctx, name)
	if err := wrapErr("inspect-bucket "+name, err); err != nil && !errors.Is(err, ErrNotFound) {
		return Bucket{}, err
	}
	if lc != nil {
		for _, r := range lc.Rules {
			if r.ID == LifecycleRule && r.Status == "Enabled" {
				b.NoncurrentDays, b.ExpireDays = int(r.NoncurrentVersionExpiration.NoncurrentDays), int(r.Expiration.Days)
			}
		}
	}
	q, err := c.admin.GetBucketQuota(ctx, name)
	if err != nil {
		return Bucket{}, wrapErr("inspect-bucket "+name, err)
	}
	if q.Type == madmin.HardQuota || q.Type == "" {
		b.QuotaBytes = q.Size
	}
	return b, nil
}

// CreateBucket creates a bucket in the client's region.
func (c *Client) CreateBucket(ctx context.Context, name string) error {
	err := c.s3.MakeBucket(ctx, name, minio.MakeBucketOptions{Region: c.region})
	if isCode(err, "BucketAlreadyOwnedByYou") {
		return nil
	}
	return wrapErr("create-bucket "+name, err)
}

// SetVersioning turns versioning on, or suspends it.
func (c *Client) SetVersioning(ctx context.Context, name string, on bool) error {
	if on {
		return wrapErr("set-versioning "+name, c.s3.EnableVersioning(ctx, name))
	}
	return wrapErr("set-versioning "+name, c.s3.SuspendVersioning(ctx, name))
}

// SetLifecycle replaces the bucket's lifecycle with rule LifecycleRule:
// noncurrent versions expire after noncurrentDays, and objects after
// expireDays unless it is 0.
func (c *Client) SetLifecycle(ctx context.Context, name string, noncurrentDays, expireDays int) error {
	rule := lifecycle.Rule{
		ID:                          LifecycleRule,
		Status:                      "Enabled",
		RuleFilter:                  lifecycle.Filter{Prefix: ""},
		NoncurrentVersionExpiration: lifecycle.NoncurrentVersionExpiration{NoncurrentDays: lifecycle.ExpirationDays(noncurrentDays)},
	}
	if expireDays > 0 {
		rule.Expiration = lifecycle.Expiration{Days: lifecycle.ExpirationDays(expireDays)}
	}
	cfg := lifecycle.NewConfiguration()
	cfg.Rules = []lifecycle.Rule{rule}
	return wrapErr("set-lifecycle "+name, c.s3.SetBucketLifecycle(ctx, name, cfg))
}

// SetQuota sets a hard quota: the store refuses the writes beyond it.
func (c *Client) SetQuota(ctx context.Context, name string, bytes uint64) error {
	return wrapErr("set-quota "+name, c.admin.SetBucketQuota(ctx, name, &madmin.BucketQuota{Size: bytes, Type: madmin.HardQuota}))
}

// BucketEmpty reports whether a bucket holds no object version and no delete
// marker. A missing bucket holds nothing.
func (c *Client) BucketEmpty(ctx context.Context, name string) (bool, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for o := range c.s3.ListObjects(ctx, name, minio.ListObjectsOptions{Recursive: true, WithVersions: true, MaxKeys: 1}) {
		if o.Err != nil {
			err := wrapErr("bucket-empty "+name, o.Err)
			if errors.Is(err, ErrNotFound) {
				return true, nil
			}
			return false, err
		}
		return false, nil
	}
	return true, nil
}

// RemoveBucket removes an empty bucket. A missing one is removed already.
func (c *Client) RemoveBucket(ctx context.Context, name string) error {
	err := wrapErr("remove-bucket "+name, c.s3.RemoveBucket(ctx, name))
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	return err
}
