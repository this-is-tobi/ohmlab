// Package rustfs manages a RustFS store's buckets and identities: MinIO's
// admin client (RustFS serves /minio/admin/ under /rustfs/admin/) and its S3
// client, over one TLS transport.
package rustfs

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/minio/madmin-go/v4"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

var (
	// ErrForbidden is the store refusing the key or what it may do.
	ErrForbidden = errors.New("access denied")
	// ErrNotFound is the store not knowing what was asked for.
	ErrNotFound = errors.New("not found")
)

// Config is where a store is and which identity to use.
type Config struct {
	// Endpoint is host:port or https://host:port.
	Endpoint string
	// CAFile verifies the store's certificate. Empty: the system roots.
	CAFile    string
	Region    string
	AccessKey string
	SecretKey string
}

// Client talks to one store as one identity.
type Client struct {
	admin  *madmin.AdminClient
	s3     *minio.Client
	region string
}

// New builds a client. It sends nothing until a method is called.
func New(cfg Config) (*Client, error) {
	host, err := hostPort(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	tr, err := transport(cfg.CAFile)
	if err != nil {
		return nil, err
	}
	creds := credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, "")
	admin, err := madmin.NewWithOptions(host, &madmin.Options{Creds: creds, Secure: true, Transport: tr})
	if err != nil {
		return nil, fmt.Errorf("admin client: %w", err)
	}
	s3, err := minio.New(host, &minio.Options{Creds: creds, Secure: true, Transport: tr, Region: cfg.Region, BucketLookup: minio.BucketLookupPath})
	if err != nil {
		return nil, fmt.Errorf("s3 client: %w", err)
	}
	return &Client{admin: admin, s3: s3, region: cfg.Region}, nil
}

func hostPort(endpoint string) (string, error) {
	host := strings.TrimPrefix(endpoint, "https://")
	if _, _, err := net.SplitHostPort(host); err != nil || strings.Contains(host, "/") {
		return "", fmt.Errorf("endpoint %q: want host:port or https://host:port", endpoint)
	}
	return host, nil
}

// transport verifies the store with caFile, over TLS 1.2 or later. It never
// uses a proxy: the storage target's egress reaches its stores only.
func transport(caFile string) (*http.Transport, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if caFile != "" {
		pem, err := os.ReadFile(caFile)
		if err != nil {
			return nil, fmt.Errorf("read the store CA: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("no certificate in %s", caFile)
		}
		tlsConfig.RootCAs = pool
	}
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		TLSClientConfig:       tlsConfig,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		MaxIdleConnsPerHost:   4,
		ForceAttemptHTTP2:     true,
	}, nil
}

var (
	forbiddenCodes = []string{"AccessDenied", "InvalidAccessKeyId", "SignatureDoesNotMatch"}
	// RustFS answers NoSuchResource for a missing user, policy or service
	// account; the XMinioAdmin codes are MinIO's.
	notFoundCodes = []string{
		"NoSuchBucket", "NoSuchBucketPolicy", "NoSuchKey", "NoSuchLifecycleConfiguration", "NoSuchResource",
		"XMinioAdminNoSuchPolicy", "XMinioAdminNoSuchServiceAccount", "XMinioAdminNoSuchUser",
	}
	// Misses RustFS answers with a generic code: delete-service-account says
	// InvalidRequest (rustfs/src/admin/handlers/service_account.rs at 1.0.5).
	notFoundMessages = map[string][]string{
		"InvalidRequest": {"service account not exist"},
	}
)

// wrapErr names the operation and keeps the store's code and message only:
// never a request, a body or a header.
func wrapErr(op string, err error) error {
	if err == nil {
		return nil
	}
	code, message, status := errorOf(err)
	switch {
	case slices.Contains(forbiddenCodes, code):
		return fmt.Errorf("%s: %w (%s)", op, ErrForbidden, code)
	case slices.Contains(notFoundCodes, code), code != "" && status == http.StatusNotFound,
		slices.Contains(notFoundMessages[code], message):
		return fmt.Errorf("%s: %w (%s)", op, ErrNotFound, code)
	case code != "":
		return fmt.Errorf("%s: %s: %s", op, code, message)
	}
	return redacted{msg: op + ": " + urlQuery.ReplaceAllString(err.Error(), "$1"), err: err}
}

// urlQuery matches a URL's query. A transport error quotes the request's URL,
// and some queries carry an access key (delete-service-account?accessKey=).
var urlQuery = regexp.MustCompile(`(https?://[^\s"?]*)\?[^\s"]*`)

// redacted is an error whose text is rewritten and whose chain is kept.
type redacted struct {
	msg string
	err error
}

func (r redacted) Error() string { return r.msg }
func (r redacted) Unwrap() error { return r.err }

func errorOf(err error) (code, message string, status int) {
	if r := madmin.ToErrorResponse(err); r.Code != "" {
		return r.Code, r.Message, r.StatusCode
	}
	if r := minio.ToErrorResponse(err); r.Code != "" {
		return r.Code, r.Message, r.StatusCode
	}
	return "", "", 0
}

func isCode(err error, code string) bool {
	c, _, _ := errorOf(err)
	return c == code
}
