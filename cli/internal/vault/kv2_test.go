package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// recorder serves canned Vault replies and remembers what it was asked.
type recorder struct {
	t        *testing.T
	handler  func(w http.ResponseWriter, r *http.Request, body []byte)
	requests []string
	bodies   []string
	tokens   []string
}

func (rec *recorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	rec.requests = append(rec.requests, r.Method+" "+r.URL.Path)
	rec.bodies = append(rec.bodies, string(b))
	rec.tokens = append(rec.tokens, r.Header.Get("X-Vault-Token"))
	rec.handler(w, r, b)
}

func newClient(t *testing.T, handler func(w http.ResponseWriter, r *http.Request, body []byte)) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{t: t, handler: handler}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	c, err := NewClient(ClientConfig{Address: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	c.UseToken("t")
	return c, rec
}

func reply(w http.ResponseWriter, status int, body string) {
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func TestKVReadExisting(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 200, `{"data":{"data":{"a":"1","big":12345678901234567890,"n":{"k":true}},"metadata":{"version":7,"custom_metadata":{"ohmlab.fr/owner":"ns/n"}}}}`)
	})
	got, err := c.KVRead(context.Background(), "homelab", "platforms/production/core/x")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Exists || got.Version != 7 || got.Metadata["ohmlab.fr/owner"] != "ns/n" {
		t.Errorf("secret = %+v", got)
	}
	// Written back unchanged, a large integer must not turn into 1.2345678901234567e19.
	out, _ := json.Marshal(got.Data)
	if !strings.Contains(string(out), `"big":12345678901234567890`) {
		t.Errorf("number lost its exact form: %s", out)
	}
}

func TestKVReadNeverWrittenMakesOneCall(t *testing.T) {
	c, rec := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 404, `{"errors":[]}`)
	})
	got, err := c.KVRead(context.Background(), "homelab", "platforms/production/core/new")
	if err != nil {
		t.Fatal(err)
	}
	if got.Exists || got.Version != 0 || got.Data != nil {
		t.Errorf("secret = %+v", got)
	}
	if len(rec.requests) != 1 {
		t.Errorf("a path that never existed needs no metadata call: %v", rec.requests)
	}
}

func TestKVReadDeletedKeepsItsVersionAndOwner(t *testing.T) {
	c, rec := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if strings.Contains(r.URL.Path, "/data/") {
			reply(w, 404, `{"data":{"data":null,"metadata":{"version":2,"deletion_time":"2026-10-17T22:35:47Z","custom_metadata":null}}}`)
			return
		}
		reply(w, 200, `{"data":{"custom_metadata":{"ohmlab.fr/owner":"ns/n"}}}`)
	})
	got, err := c.KVRead(context.Background(), "homelab", "platforms/production/core/gone")
	if err != nil {
		t.Fatal(err)
	}
	if got.Exists || got.Version != 2 || got.Metadata["ohmlab.fr/owner"] != "ns/n" {
		t.Errorf("secret = %+v (calls %v)", got, rec.requests)
	}
}

func TestKVWriteCASSendsTheVersion(t *testing.T) {
	c, rec := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 200, `{"data":{"version":5}}`)
	})
	v, err := c.KVWriteCAS(context.Background(), "homelab", "platforms/p/c/x", map[string]any{"k": "v"}, 4)
	if err != nil || v != 5 {
		t.Fatalf("v=%d err=%v", v, err)
	}
	var sent struct {
		Options struct {
			CAS int64 `json:"cas"`
		} `json:"options"`
		Data map[string]any `json:"data"`
	}
	_ = json.Unmarshal([]byte(rec.bodies[0]), &sent)
	if sent.Options.CAS != 4 || sent.Data["k"] != "v" || rec.requests[0] != "POST /v1/homelab/data/platforms/p/c/x" {
		t.Errorf("sent %s to %s", rec.bodies[0], rec.requests[0])
	}
}

func TestKVWriteCASReportsALostRace(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 400, `{"errors":["check-and-set parameter did not match the current version"]}`)
	})
	_, err := c.KVWriteCAS(context.Background(), "homelab", "platforms/p/c/x", map[string]any{}, 0)
	if !errors.Is(err, ErrCASMismatch) {
		t.Errorf("err = %v", err)
	}
}

func TestOtherBadRequestsAreNotCASMismatches(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 400, `{"errors":["no data provided"]}`)
	})
	_, err := c.KVWriteCAS(context.Background(), "homelab", "platforms/p/c/x", map[string]any{}, 0)
	if err == nil || errors.Is(err, ErrCASMismatch) {
		t.Errorf("err = %v", err)
	}
}

func TestKVSetCustomMetadataAndDestroy(t *testing.T) {
	c, rec := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Method == http.MethodDelete {
			reply(w, 404, `{"errors":[]}`)
			return
		}
		w.WriteHeader(204)
	})
	if err := c.KVSetCustomMetadata(context.Background(), "homelab", "platforms/p/c/x", map[string]string{"ohmlab.fr/owner": "ns/n"}); err != nil {
		t.Fatal(err)
	}
	if rec.requests[0] != "POST /v1/homelab/metadata/platforms/p/c/x" || rec.bodies[0] != `{"custom_metadata":{"ohmlab.fr/owner":"ns/n"}}` {
		t.Errorf("sent %s %s", rec.requests[0], rec.bodies[0])
	}
	if err := c.KVDestroy(context.Background(), "homelab", "platforms/p/c/x"); err != nil {
		t.Errorf("destroying a path that is already gone is not an error: %v", err)
	}
}

func TestA403GetsOneFreshLoginThenSucceeds(t *testing.T) {
	var logins atomic.Int32
	c, rec := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if r.Header.Get("X-Vault-Token") == "old" {
			reply(w, 403, `{"errors":["permission denied","invalid token"]}`)
			return
		}
		reply(w, 200, `{"data":{"data":{"a":"1"},"metadata":{"version":1}}}`)
	})
	c.session = &session{token: "old", until: time.Now().Add(time.Hour), login: func(context.Context) (string, time.Duration, error) {
		logins.Add(1)
		return "new", time.Minute, nil
	}}
	got, err := c.KVRead(context.Background(), "homelab", "platforms/p/c/x")
	if err != nil || !got.Exists {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	if logins.Load() != 1 || rec.tokens[len(rec.tokens)-1] != "new" {
		t.Errorf("logins=%d tokens=%v", logins.Load(), rec.tokens)
	}
}

func TestARefusedPolicyIsForbiddenAfterOneRelogin(t *testing.T) {
	var logins atomic.Int32
	c, rec := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 403, `{"errors":["permission denied"]}`)
	})
	c.session = &session{login: func(context.Context) (string, time.Duration, error) {
		logins.Add(1)
		return fmt.Sprintf("tok%d", logins.Load()), time.Minute, nil
	}}
	_, err := c.KVRead(context.Background(), "homelab", "other/path")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("err = %v", err)
	}
	if logins.Load() != 2 || len(rec.requests) != 2 {
		t.Errorf("want the first login plus one fresh login and two requests, got %d logins and %d requests", logins.Load(), len(rec.requests))
	}
}

func TestTheTokenIsReplacedBeforeItRunsOut(t *testing.T) {
	var logins atomic.Int32
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 200, `{"data":{"data":{},"metadata":{"version":1}}}`)
	})
	c.session = &session{login: func(context.Context) (string, time.Duration, error) {
		logins.Add(1)
		return "tok", 15 * time.Minute, nil
	}}
	for i := 0; i < 3; i++ {
		if _, err := c.KVRead(context.Background(), "homelab", "platforms/p/c/x"); err != nil {
			t.Fatal(err)
		}
	}
	if logins.Load() != 1 {
		t.Fatalf("a live token is reused: %d logins", logins.Load())
	}
	if remaining := time.Until(c.session.until); remaining < 9*time.Minute || remaining > 11*time.Minute {
		t.Errorf("the next login is due two thirds into a 15m token, got %s", remaining)
	}
	c.session.until = time.Now().Add(-time.Second)
	if _, err := c.KVRead(context.Background(), "homelab", "platforms/p/c/x"); err != nil {
		t.Fatal(err)
	}
	if logins.Load() != 2 {
		t.Errorf("an aged token must be replaced: %d logins", logins.Load())
	}
}

func TestAStaticTokenIsNeverRefreshedAndA403IsForbiddenAtOnce(t *testing.T) {
	c, rec := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 403, `{"errors":["permission denied"]}`)
	})
	_, err := c.KVRead(context.Background(), "homelab", "x/y")
	if !errors.Is(err, ErrForbidden) || len(rec.requests) != 1 {
		t.Errorf("err=%v requests=%v", err, rec.requests)
	}
}

func TestTransientFailuresAreRetried(t *testing.T) {
	var n atomic.Int32
	c, rec := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if n.Add(1) < 3 {
			reply(w, 429, `{"errors":["rate limit"]}`)
			return
		}
		reply(w, 200, `{"data":{"data":{},"metadata":{"version":1}}}`)
	})
	if _, err := c.KVRead(context.Background(), "homelab", "x/y"); err != nil {
		t.Fatal(err)
	}
	if len(rec.requests) != 3 {
		t.Errorf("requests = %v", rec.requests)
	}
}

func TestTheBackoffGivesUpWithTheContext(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 503, `{"errors":["sealed"]}`)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := c.KVRead(ctx, "homelab", "x/y"); err == nil {
		t.Fatal("want an error")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("took %s", time.Since(start))
	}
}

func TestErrorsNeverCarryTheRequestBody(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request, b []byte) {
		reply(w, 500, `{"errors":["internal error"]}`)
	})
	_, err := c.KVWriteCAS(context.Background(), "homelab", "x/y", map[string]any{"password": "TOP-SECRET"}, 0)
	if err == nil || strings.Contains(err.Error(), "TOP-SECRET") {
		t.Errorf("err = %v", err)
	}
}

func TestKubernetesLoginSendsTheRoleAndTheTrimmedToken(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("jwt-value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, rec := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		if strings.HasSuffix(r.URL.Path, "/login") {
			reply(w, 200, `{"auth":{"client_token":"s.abc","lease_duration":900}}`)
			return
		}
		reply(w, 200, `{"data":{"data":{},"metadata":{"version":1}}}`)
	})
	c.UseKubernetesLogin("", "ohmlab-controller-vault", tokenFile)
	if _, err := c.KVRead(context.Background(), "homelab", "x/y"); err != nil {
		t.Fatal(err)
	}
	if rec.requests[0] != "POST /v1/auth/kubernetes/login" || rec.bodies[0] != `{"jwt":"jwt-value","role":"ohmlab-controller-vault"}` {
		t.Errorf("login = %s %s", rec.requests[0], rec.bodies[0])
	}
	if rec.tokens[1] != "s.abc" {
		t.Errorf("the granted token was not used: %v", rec.tokens)
	}
}

func TestALoginWithoutATokenFails(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 200, `{"auth":{}}`)
	})
	c.UseKubernetesLogin("", "r", writeTemp(t, "j"))
	if _, err := c.KVRead(context.Background(), "homelab", "x/y"); err == nil {
		t.Error("want an error")
	}
}

func TestARefusedLoginIsForbiddenAndSaysWhy(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 403, `{"errors":["service account name not authorized"]}`)
	})
	c.UseKubernetesLogin("", "r", writeTemp(t, "j"))
	_, err := c.KVRead(context.Background(), "homelab", "x/y")
	if !errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "service account name not authorized") {
		t.Errorf("a refused login must be ErrForbidden and carry Vault's reason: %v", err)
	}
}

func TestALoginTheServerRejectsReportsItsStatus(t *testing.T) {
	c, _ := newClient(t, func(w http.ResponseWriter, r *http.Request, _ []byte) {
		reply(w, 400, `{"errors":["role \"r\" could not be found"]}`)
	})
	c.UseKubernetesLogin("", "r", writeTemp(t, "j"))
	_, err := c.KVRead(context.Background(), "homelab", "x/y")
	if err == nil || errors.Is(err, ErrForbidden) || !strings.Contains(err.Error(), "status 400") || !strings.Contains(err.Error(), "could not be found") {
		t.Errorf("a rejected login must report its status and reason, not a missing token: %v", err)
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}
