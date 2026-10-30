package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/this-is-tobi/ohmlab/utils/internal/vault"
)

// A real Vault, for the tests that check what the in-memory KV only imitates.
// Set OHMLAB_TEST_VAULT_ADDR and OHMLAB_TEST_VAULT_TOKEN (a root token) to run
// them, for example against `vault server -dev`. Every test works in a mount of
// its own, so one server serves them all.

var mountCounter atomic.Int64

type devVault struct {
	t    *testing.T
	addr string
	root string
}

func devVaultOrSkip(t *testing.T) *devVault {
	t.Helper()
	addr, root := os.Getenv("OHMLAB_TEST_VAULT_ADDR"), os.Getenv("OHMLAB_TEST_VAULT_TOKEN")
	if addr == "" || root == "" {
		t.Skip("OHMLAB_TEST_VAULT_ADDR and OHMLAB_TEST_VAULT_TOKEN are not set")
	}
	return &devVault{t: t, addr: strings.TrimRight(addr, "/"), root: root}
}

func (d *devVault) call(method, path string, body any) (int, []byte) {
	d.t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, d.addr+"/v1/"+path, r)
	req.Header.Set("X-Vault-Token", d.root)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		d.t.Fatal(err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func (d *devVault) must(method, path string, body any) []byte {
	d.t.Helper()
	status, out := d.call(method, path, body)
	if status >= 300 {
		d.t.Fatalf("%s %s: %d %s", method, path, status, out)
	}
	return out
}

// mount enables a KV v2 mount named after the test and returns its name and a
// client holding a token that has only the controller's policy on it.
func (d *devVault) mount() (mount string, controller *vault.Client) {
	d.t.Helper()
	mount = fmt.Sprintf("it%d%d", time.Now().UnixNano()%1_000_000, mountCounter.Add(1))
	d.must("POST", "sys/mounts/"+mount, map[string]any{"type": "kv", "options": map[string]string{"version": "2"}})

	hcl, err := os.ReadFile("testdata/controller-policy.hcl")
	if err != nil {
		d.t.Fatal(err)
	}
	d.must("PUT", "sys/policies/acl/"+mount, map[string]string{"policy": strings.ReplaceAll(string(hcl), "homelab/", mount+"/")})

	c, err := vault.NewClient(vault.ClientConfig{Address: d.addr})
	if err != nil {
		d.t.Fatal(err)
	}
	c.UseToken(d.controllerToken(mount))
	return mount, c
}

func (d *devVault) controllerToken(policy string) string {
	d.t.Helper()
	out := d.must("POST", "auth/token/create", map[string]any{"policies": []string{policy}, "ttl": "15m", "no_default_policy": false})
	var r struct {
		Auth struct {
			ClientToken string `json:"client_token"`
		} `json:"auth"`
	}
	_ = json.Unmarshal(out, &r)
	return r.Auth.ClientToken
}

// rootPut writes a secret as a human with a root token would.
func (d *devVault) rootPut(mount, path string, data map[string]any) {
	d.t.Helper()
	d.must("POST", mount+"/data/"+path, map[string]any{"data": data})
}

func (d *devVault) rootRead(mount, path string) (data map[string]any, version int64, custom map[string]string) {
	d.t.Helper()
	out := d.must("GET", mount+"/data/"+path, nil)
	var r struct {
		Data struct {
			Data     map[string]any `json:"data"`
			Metadata struct {
				Version        int64             `json:"version"`
				CustomMetadata map[string]string `json:"custom_metadata"`
			} `json:"metadata"`
		} `json:"data"`
	}
	_ = json.Unmarshal(out, &r)
	return r.Data.Data, r.Data.Metadata.Version, r.Data.Metadata.CustomMetadata
}

func background() context.Context { return context.Background() }
