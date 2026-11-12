package rustfs

import (
	"errors"
	"net/http"
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
