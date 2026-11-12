package rustfs

import (
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/minio/madmin-go/v4"
	"github.com/minio/minio-go/v7"
)

func TestWrapErrClassifiesTheStoresAnswers(t *testing.T) {
	admin := func(code, message string, status int) error {
		return madmin.ErrorResponse{Code: code, Message: message, StatusCode: status, RequestID: "req-123", HostID: "host-456"}
	}
	tests := []struct {
		name                string
		err                 error
		forbidden, notFound bool
	}{
		{"access denied", admin("AccessDenied", "Access Denied.", http.StatusForbidden), true, false},
		{"an unknown key", minio.ErrorResponse{Code: "InvalidAccessKeyId", StatusCode: http.StatusForbidden}, true, false},
		{"a bad signature", minio.ErrorResponse{Code: "SignatureDoesNotMatch", StatusCode: http.StatusForbidden}, true, false},
		{"a missing user or policy", admin("NoSuchResource", "user does not exist", http.StatusNotFound), false, true},
		{"a missing bucket", minio.ErrorResponse{Code: "NoSuchBucket", StatusCode: http.StatusNotFound}, false, true},
		{"a missing service account", admin("InvalidRequest", "service account not exist", http.StatusBadRequest), false, true},
		{"another invalid request", admin("InvalidRequest", "iam not init", http.StatusBadRequest), false, false},
		{"a quota refusal", minio.ErrorResponse{Code: "XRustfsQuotaExceeded", Message: "Bucket quota exceeded", StatusCode: http.StatusBadRequest}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := wrapErr("op", tt.err)
			if got := errors.Is(err, ErrForbidden); got != tt.forbidden {
				t.Errorf("ErrForbidden = %v, want %v (%v)", got, tt.forbidden, err)
			}
			if got := errors.Is(err, ErrNotFound); got != tt.notFound {
				t.Errorf("ErrNotFound = %v, want %v (%v)", got, tt.notFound, err)
			}
			if msg := err.Error(); !strings.HasPrefix(msg, "op: ") || strings.Contains(msg, "req-123") || strings.Contains(msg, "host-456") {
				t.Errorf("message %q: want the op first and no request or host id", msg)
			}
		})
	}
	if wrapErr("op", nil) != nil {
		t.Error("wrapErr(nil) must be nil")
	}
}

func TestWrapErrDropsTheQueryOfATransportError(t *testing.T) {
	key := rand.Text()
	u := "https://rustfs-svc.store.svc:9000/rustfs/admin/v3/delete-service-account?accessKey=" + key
	for name, in := range map[string]error{
		"a refused connection": &url.Error{Op: "Delete", URL: u, Err: errors.New("dial tcp 10.0.0.1:9000: connect: connection refused")},
		"madmin's EOF rewrite": &url.Error{Op: "Delete", URL: u, Err: errors.New("Connection closed by foreign host " + u + ". Retry again.")},
	} {
		err := wrapErr("remove-service-account", in)
		if msg := err.Error(); strings.Contains(msg, key) || !strings.Contains(msg, "/rustfs/admin/v3/delete-service-account") {
			t.Errorf("%s: message %q: want the URL's path and no query", name, msg)
		}
		if ue := (*url.Error)(nil); !errors.As(err, &ue) {
			t.Errorf("%s: the transport error must stay in the chain", name)
		}
	}
}

// storeAnswering is a store that answers every request with status and body.
func storeAnswering(t *testing.T, status int, body string) *Client {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	ca := filepath.Join(t.TempDir(), "ca.crt")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := New(Config{Endpoint: srv.Listener.Addr().String(), CAFile: ca, Region: "us-east-1", AccessKey: "TESTACCESSKEY", SecretKey: "test-secret-key"})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestBucketEmptyOfAMissingBucketIsTrue(t *testing.T) {
	c := storeAnswering(t, http.StatusNotFound,
		`<?xml version="1.0" encoding="UTF-8"?><Error><Code>NoSuchBucket</Code><Message>The specified bucket does not exist</Message><BucketName>gone</BucketName></Error>`)
	if empty, err := c.BucketEmpty(t.Context(), "gone"); err != nil || !empty {
		t.Errorf("BucketEmpty of a missing bucket = %v, %v; want true, so a deletion that removed it already can finish", empty, err)
	}
}
