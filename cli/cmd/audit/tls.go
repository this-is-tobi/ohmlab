package audit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"github.com/this-is-tobi/ohmlab/cli/internal/config"
	"github.com/this-is-tobi/ohmlab/cli/internal/kubeclient"
	"github.com/this-is-tobi/ohmlab/cli/internal/tlsaudit"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

var tlsFlags struct {
	kubeconfig  string
	context     string
	config      string
	output      string
	concurrency int
	timeout     time.Duration
	all         bool
}

var tlsCmd = &cobra.Command{
	Use:   "tls",
	Short: "Find the in-cluster ports that do not speak TLS",
	Long: `Probe every TCP port of every Service through a port-forward (a headless
Service for the pod ports no cluster IP reaches), and check the answers against
the rule: credentials and data travel over TLS. A probe is a TLS hello, which an
SSH server answers with its banner (SSH encrypts by itself); for Postgres, an
SSLRequest and a plaintext startup, which pg_hba refuses or lets through before
any password.

Metrics ports (named so, or on a Service named so; not Loki's API port), DNS,
and the exceptions of the workspace file (audit.tls.exceptions in ohmlab.yaml,
each with a reason) may stay plaintext. Exits non-zero when a port is plaintext
with nothing allowing it.

Needs to list Services and EndpointSlices, to read kube-system/cilium-config
and to create pods/portforward.`,
	Args: cobra.NoArgs,
	RunE: runTLS,
}

func init() {
	f := tlsCmd.Flags()
	f.StringVar(&tlsFlags.kubeconfig, "kubeconfig", "", "path to kubeconfig (default: $KUBECONFIG, then ~/.kube/config)")
	f.StringVar(&tlsFlags.context, "context", "", "kubeconfig context (default: the current one)")
	f.StringVar(&tlsFlags.config, "config", config.DefaultPath, "workspace file with the exceptions; optional unless set")
	f.StringVarP(&tlsFlags.output, "output", "o", "text", "text or json")
	f.IntVar(&tlsFlags.concurrency, "concurrency", 4, "ports probed at a time")
	f.DurationVar(&tlsFlags.timeout, "timeout", 5*time.Second, "time a port has to answer")
	f.BoolVar(&tlsFlags.all, "all", false, "list every port, not only the ones that need attention")
	Cmd.AddCommand(tlsCmd)
}

func runTLS(cmd *cobra.Command, _ []string) error {
	if tlsFlags.output != "text" && tlsFlags.output != "json" {
		return fmt.Errorf("--output %q: want text or json", tlsFlags.output)
	}
	var exceptions []config.TLSException
	file, err := config.Load(tlsFlags.config)
	switch {
	case err == nil:
		exceptions = file.Audit.TLS.Exceptions
	case errors.Is(err, fs.ErrNotExist) && !cmd.Flags().Changed("config"):
	default:
		return err
	}

	cfg, contextName, err := kubeclient.Load(tlsFlags.kubeconfig, tlsFlags.context)
	if err != nil {
		return err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return err
	}
	// Every probe closes its forwarded stream early, which client-go reports
	// as an error; the verdicts carry what matters.
	klog.SetLogger(logr.Discard())
	ctx := cmd.Context()
	targets, err := tlsaudit.Discover(ctx, cs)
	if err != nil {
		return err
	}
	wireguard, err := tlsaudit.WireGuard(ctx, cs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v; reporting WireGuard as off\n", err)
	}
	forward, err := tlsaudit.NewForwarder(cfg, cs)
	if err != nil {
		return err
	}
	findings := tlsaudit.Run(ctx, targets, forward, tlsaudit.Options{Concurrency: tlsFlags.concurrency, Timeout: tlsFlags.timeout})
	findings, stale := tlsaudit.Evaluate(findings, exceptions)
	report := tlsaudit.Report{Context: contextName, WireGuard: wireguard, Findings: findings, Stale: stale}

	if tlsFlags.output == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		err = enc.Encode(report)
	} else {
		err = tlsaudit.WriteText(os.Stdout, report, tlsFlags.all)
	}
	if err != nil {
		return err
	}
	if len(report.Violations()) > 0 {
		os.Exit(1)
	}
	return nil
}
