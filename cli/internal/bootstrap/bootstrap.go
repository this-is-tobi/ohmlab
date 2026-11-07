package bootstrap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/this-is-tobi/ohmlab/cli/internal/runner"
	"github.com/this-is-tobi/ohmlab/cli/internal/ui"
)

// ArgoCDMarker is Argo CD's Application CRD: when it is absent, Argo CD is not
// installed, and phase 1 installs it alone first.
const ArgoCDMarker = "applications.argoproj.io"

var httpClient = &http.Client{Timeout: 2 * time.Minute}

// Kube is where Helm acts: the context the CLI checked, never whichever one
// is current.
type Kube struct{ Kubeconfig, Context string }

func (k Kube) args() []string {
	a := []string{"--kube-context", k.Context}
	if k.Kubeconfig != "" {
		a = append(a, "--kubeconfig", k.Kubeconfig)
	}
	return a
}

// Options are the inputs of a bootstrap.
type Options struct {
	Chart   Chart
	Release Release
	// Values is the instance's values/core/ohmlab.yaml.
	Values        string
	Kube          Kube
	GatewayAPIURL string
}

// Plan is what a bootstrap does to a cluster, decided from what it holds.
type Plan struct {
	// GatewayAPI: the Gateway API CRDs are missing, and get installed.
	GatewayAPI bool
	// Phase1: Argo CD is missing, and gets installed alone first: the
	// chart's AppProjects and ApplicationSet need the CRDs it brings.
	Phase1 bool
}

// Decide reads the plan from the cluster.
func Decide(ctx context.Context, c Cluster) (Plan, error) {
	gateway, err := c.HasCRD(ctx, GatewayAPIMarker)
	if err != nil {
		return Plan{}, err
	}
	argo, err := c.HasCRD(ctx, ArgoCDMarker)
	if err != nil {
		return Plan{}, err
	}
	return Plan{GatewayAPI: !gateway, Phase1: !argo}, nil
}

// Step is one helm command of a bootstrap.
type Step struct {
	Title string
	Cmd   runner.Cmd
}

// Commands are the helm commands of the plan, in order. passwordValues is the
// values file setting the admin password, if any.
func Commands(o Options, p Plan, passwordValues string) []Step {
	var steps []Step
	if o.Chart.Local() {
		steps = append(steps, Step{"Update the chart's dependencies", runner.Cmd{Name: "helm", Args: []string{"dependency", "update", o.Chart.Ref}}})
	}
	install := func(set ...string) runner.Cmd {
		args := []string{"upgrade", "--install", o.Release.Name, o.Chart.Ref}
		if !o.Chart.Local() {
			args = append(args, "--version", o.Chart.Version)
		}
		args = append(args, "--namespace", o.Release.Namespace, "--create-namespace", "--values", o.Values)
		args = append(append(args, set...), "--wait", "--timeout", "10m")
		return runner.Cmd{Name: "helm", Args: append(args, o.Kube.args()...)}
	}
	if p.Phase1 {
		steps = append(steps, Step{"Phase 1: install Argo CD alone", install(
			"--set", "rootManager.enabled=false", "--set", "projects.core.enabled=false", "--set", "projects.tenant.enabled=false",
			"--set", "gateway.enabled=false", "--set", "vso.enabled=false")})
	}
	var set []string
	if passwordValues != "" {
		set = append(set, "--values", passwordValues)
	}
	// VSO arrives later, with the core apps. Forcing takes back the fields the
	// self-managed Application has applied since an earlier bootstrap.
	set = append(set, "--set", "vso.enabled=false", "--force-conflicts")
	return append(steps, Step{"Phase 2: install the ohmlab chart", install(set...)})
}

// Summary is the chart, release, Gateway API and Argo CD part of a plan.
func Summary(o Options, p Plan) []string {
	chart := "Chart " + o.Chart.Ref + " " + o.Chart.Version
	if o.Chart.Local() {
		chart = "Chart " + o.Chart.Ref + " (a directory)"
	}
	gateway := "Gateway API CRDs: present"
	if p.GatewayAPI {
		gateway = "Gateway API CRDs: missing, installs " + GatewayAPIVersion + " (experimental channel)"
	}
	argo := "Argo CD: installed, phase 1 skipped"
	if p.Phase1 {
		argo = "Argo CD: missing, phase 1 installs it alone first"
	}
	return []string{chart, "Release " + o.Release.Name + " in " + o.Release.Namespace, gateway, argo}
}

// CheckHelm makes sure helm is installed and is Helm 4 or later, whose
// server-side apply phase 2's --force-conflicts belongs to.
func CheckHelm(ctx context.Context, r runner.Runner) error {
	if _, err := r.LookPath("helm"); err != nil {
		return errors.New("helm is not installed: bootstrap runs it (Helm 4)")
	}
	var out bytes.Buffer
	if err := r.Run(ctx, runner.Cmd{Name: "helm", Args: []string{"version", "--template", "{{.Version}}"}, Stdout: &out}); err != nil {
		return err
	}
	v := strings.TrimSpace(out.String())
	major, _, _ := strings.Cut(strings.TrimPrefix(v, "v"), ".")
	if n, err := strconv.Atoi(major); err != nil || n < 4 {
		return fmt.Errorf("helm %s is too old: bootstrap needs Helm 4", v)
	}
	return nil
}

// Run carries the plan out: the Gateway API CRDs when they are missing, then
// the helm commands. The admin password's values file lives in a private
// directory that is gone when Run returns.
func Run(ctx context.Context, r runner.Runner, c Cluster, u *ui.UI, o Options, p Plan, password string) error {
	if p.GatewayAPI {
		step := u.Step("Install the Gateway API CRDs (" + GatewayAPIVersion + ", experimental channel)")
		if err := step.Done(installGatewayAPI(ctx, c, o.GatewayAPIURL)); err != nil {
			return err
		}
	}
	var passwordValues string
	if password != "" {
		dir, err := os.MkdirTemp("", "ohmlab-bootstrap-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		if passwordValues, err = WriteAdminPasswordValues(dir, password); err != nil {
			return err
		}
	}
	for _, s := range Commands(o, p, passwordValues) {
		step := u.Step(s.Title)
		if err := step.Done(r.Run(ctx, s.Cmd)); err != nil {
			return err
		}
	}
	return nil
}

func installGatewayAPI(ctx context.Context, c Cluster, url string) error {
	objs, err := FetchManifests(ctx, httpClient, url)
	if err != nil {
		return err
	}
	if err := c.Apply(ctx, objs); err != nil {
		return err
	}
	var crds []string
	for _, o := range objs {
		if o.GetKind() == "CustomResourceDefinition" {
			crds = append(crds, o.GetName())
		}
	}
	return c.WaitEstablished(ctx, crds, time.Minute)
}
