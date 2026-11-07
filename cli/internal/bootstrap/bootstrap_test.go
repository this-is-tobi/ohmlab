package bootstrap_test

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	bs "github.com/this-is-tobi/ohmlab/cli/internal/bootstrap"
	"github.com/this-is-tobi/ohmlab/cli/internal/bootstrap/bootstraptest"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/runner/runnertest"
	"github.com/this-is-tobi/ohmlab/cli/internal/ui"
)

const bundle = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: tlsroutes.gateway.networking.k8s.io
---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: httproutes.gateway.networking.k8s.io
`

const (
	dependencies = "helm dependency update /p/charts/ohmlab"
	phase1       = "helm upgrade --install ohmlab /p/charts/ohmlab --namespace argocd-system --create-namespace --values /i/values/core/ohmlab.yaml --set rootManager.enabled=false --set projects.core.enabled=false --set projects.tenant.enabled=false --set gateway.enabled=false --set vso.enabled=false --wait --timeout 10m --kube-context ctx"
	phase2       = "helm upgrade --install ohmlab /p/charts/ohmlab --namespace argocd-system --create-namespace --values /i/values/core/ohmlab.yaml --set vso.enabled=false --force-conflicts --wait --timeout 10m --kube-context ctx"
)

// bundleServer serves the Gateway API bundle and counts the requests.
func bundleServer(t *testing.T) (string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, bundle)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}

// local is a bootstrap of the product's chart directory into context ctx.
func local(url string) bs.Options {
	return bs.Options{Chart: bs.Chart{Ref: "/p/charts/ohmlab"}, Release: bs.Release{Name: "ohmlab", Namespace: "argocd-system"},
		Values: "/i/values/core/ohmlab.yaml", Kube: bs.Kube{Context: "ctx"}, GatewayAPIURL: url}
}

func quiet() *ui.UI { return ui.New(ui.Options{Out: io.Discard, Err: io.Discard, Mode: ui.JSON}) }

func lines(calls []runner.Cmd) []string {
	var out []string
	for _, c := range calls {
		out = append(out, c.String())
	}
	return out
}

func installed() *bootstraptest.Cluster {
	return &bootstraptest.Cluster{CRDs: map[string]bool{bs.GatewayAPIMarker: true, bs.ArgoCDMarker: true}}
}

func TestEmptyClusterRunsEverything(t *testing.T) {
	ctx := context.Background()
	url, hits := bundleServer(t)
	r, c := &runnertest.Fake{}, &bootstraptest.Cluster{}
	p, err := bs.Decide(ctx, c)
	if err != nil || p != (bs.Plan{GatewayAPI: true, Phase1: true}) {
		t.Fatalf("got %+v, %v; want both the Gateway API and phase 1", p, err)
	}
	if err := bs.Run(ctx, r, c, quiet(), local(url), p, ""); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || len(c.Applied) != 2 {
		t.Errorf("%d fetches and %d objects applied, want 1 and 2", hits.Load(), len(c.Applied))
	}
	if want := []string{"tlsroutes.gateway.networking.k8s.io", "httproutes.gateway.networking.k8s.io"}; !slices.Equal(c.Waited, want) {
		t.Errorf("waited for %v, want %v", c.Waited, want)
	}
	if got, want := lines(r.Calls), []string{dependencies, phase1, phase2}; !slices.Equal(got, want) {
		t.Errorf("ran\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRerunSkipsWhatIsInstalled(t *testing.T) {
	ctx := context.Background()
	url, hits := bundleServer(t)
	r, c := &runnertest.Fake{}, installed()
	o := local(url)
	o.Chart = bs.Chart{Ref: "oci://ghcr.io/example/charts/ohmlab", Version: "0.6.2"}
	p, err := bs.Decide(ctx, c)
	if err != nil || p != (bs.Plan{}) {
		t.Fatalf("got %+v, %v; want nothing but phase 2", p, err)
	}
	if err := bs.Run(ctx, r, c, quiet(), o, p, ""); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 || len(c.Applied) != 0 {
		t.Errorf("%d fetches and %d objects applied, want none", hits.Load(), len(c.Applied))
	}
	want := "helm upgrade --install ohmlab oci://ghcr.io/example/charts/ohmlab --version 0.6.2 --namespace argocd-system --create-namespace --values /i/values/core/ohmlab.yaml --set vso.enabled=false --force-conflicts --wait --timeout 10m --kube-context ctx"
	if got := lines(r.Calls); !slices.Equal(got, []string{want}) {
		t.Errorf("ran %q, want phase 2 alone: %q", got, want)
	}
}

func TestEveryHelmCommandTargetsTheCheckedContext(t *testing.T) {
	for _, kube := range []bs.Kube{{Context: "ctx"}, {Context: "ctx", Kubeconfig: "/k"}} {
		o := local("")
		o.Kube = kube
		want := " --kube-context ctx"
		if kube.Kubeconfig != "" {
			want += " --kubeconfig /k"
		}
		for _, s := range bs.Commands(o, bs.Plan{GatewayAPI: true, Phase1: true}, "/t/admin-password.yaml") {
			if s.Cmd.Args[0] == "upgrade" && !strings.HasSuffix(s.Cmd.String(), want) {
				t.Errorf("%q (%s) does not end with %q", s.Cmd.String(), s.Title, want)
			}
		}
	}
}

func TestPasswordValuesAreRemoved(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var values string
		r := &runnertest.Fake{Handle: func(c runner.Cmd) error {
			if !slices.Contains(c.Args, "--force-conflicts") {
				return nil
			}
			var files []string
			for i, a := range c.Args {
				if a == "--values" && i+1 < len(c.Args) {
					files = append(files, c.Args[i+1])
				}
			}
			if len(files) != 2 {
				t.Errorf("phase 2 takes values %v, want the instance's and the password's", files)
				return nil
			}
			values = files[1]
			if st, err := os.Stat(values); err != nil || st.Mode().Perm() != 0o600 {
				t.Errorf("the password's values file during phase 2: %v, %v; want mode 0600", st, err)
			}
			if fail {
				return &runner.ExitError{Name: "helm", Code: 1, Stderr: "Error: boom"}
			}
			return nil
		}}
		err := bs.Run(context.Background(), r, installed(), quiet(), local(""), bs.Plan{}, "s3cret-pw")
		if (err != nil) != fail {
			t.Errorf("phase 2 failing %v: Run returned %v", fail, err)
		}
		if values == "" {
			t.Fatal("phase 2 never ran with the password's values")
		}
		if !strings.HasPrefix(filepath.Base(filepath.Dir(values)), "ohmlab-bootstrap-") {
			t.Errorf("values file %s, want it in an ohmlab-bootstrap-* directory", values)
		}
		if _, err := os.Stat(filepath.Dir(values)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("phase 2 failing %v: %s outlived Run (%v)", fail, filepath.Dir(values), err)
		}
		for _, c := range r.Calls {
			if s := c.String(); strings.Contains(s, "s3cret-pw") || strings.Contains(s, "$2a$") {
				t.Errorf("a command line carries the password or its hash: %s", s)
			}
		}
	}
}

func TestCheckHelmRefusesHelm3(t *testing.T) {
	for _, tc := range []struct {
		version, want string
		missing       bool
	}{
		{version: "v3.17.0", want: "Helm 4"},
		{version: "v4.3.1"},
		{version: "v5.0.0"},
		{missing: true, want: "helm is not installed"},
	} {
		r := &runnertest.Fake{Handle: func(c runner.Cmd) error {
			_, err := io.WriteString(c.Stdout, tc.version)
			return err
		}}
		if tc.missing {
			r.Missing = []string{"helm"}
		}
		err := bs.CheckHelm(context.Background(), r)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("%s: got %v, want no error", tc.version, err)
		case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
			t.Errorf("%s: got %v, want an error with %q", tc.version, err, tc.want)
		}
		if !tc.missing && (len(r.Calls) != 1 || r.Calls[0].String() != "helm version --template '{{.Version}}'") {
			t.Errorf("ran %q, want helm version alone", lines(r.Calls))
		}
	}
}

func TestAFailedPhaseStopsTheRun(t *testing.T) {
	r := &runnertest.Fake{Handle: func(c runner.Cmd) error {
		if slices.Contains(c.Args, "rootManager.enabled=false") {
			return &runner.ExitError{Name: "helm", Code: 1, Stderr: "Error: context deadline exceeded"}
		}
		return nil
	}}
	o := local("")
	o.Chart = bs.Chart{Ref: "oci://ghcr.io/example/charts/ohmlab", Version: "0.6.2"}
	c := &bootstraptest.Cluster{CRDs: map[string]bool{bs.GatewayAPIMarker: true}}
	err := bs.Run(context.Background(), r, c, quiet(), o, bs.Plan{Phase1: true}, "")
	var exit *runner.ExitError
	if !errors.As(err, &exit) {
		t.Errorf("got %v, want helm's ExitError", err)
	}
	if len(r.Calls) != 1 {
		t.Errorf("ran %q, want phase 1 alone", lines(r.Calls))
	}
}

func TestHelmGetsTimeToStop(t *testing.T) {
	for _, s := range bs.Commands(local(""), bs.Plan{GatewayAPI: true, Phase1: true}, "/t/admin-password.yaml") {
		if s.Cmd.Grace != 30*time.Second {
			t.Errorf("%s: grace %s, want 30s for helm to mark a release it stops as failed", s.Title, s.Cmd.Grace)
		}
	}
}

func TestPhase1SetsTheGivenPassword(t *testing.T) {
	var phase1 string
	for _, s := range bs.Commands(local(""), bs.Plan{Phase1: true}, "/t/admin-password.yaml") {
		if slices.Contains(s.Cmd.Args, "rootManager.enabled=false") {
			phase1 = s.Cmd.String()
		}
	}
	if !strings.Contains(phase1, "--values /i/values/core/ohmlab.yaml --values /t/admin-password.yaml --set rootManager.enabled=false") {
		t.Errorf("phase 1 is %q, want the password's values after the instance's: Argo CD then generates no password of its own", phase1)
	}
}

func TestRunRefusesAnEmptyContext(t *testing.T) {
	r := &runnertest.Fake{}
	o := local("")
	o.Kube = bs.Kube{}
	err := bs.Run(context.Background(), r, installed(), quiet(), o, bs.Plan{}, "")
	if err == nil || !strings.Contains(err.Error(), "context") {
		t.Errorf("got %v, want a refusal: helm reads an empty --kube-context as the current one", err)
	}
	if len(r.Calls) != 0 {
		t.Errorf("ran %q", lines(r.Calls))
	}
}
