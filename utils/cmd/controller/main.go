// Command ohmlab-controller reconciles ohmlab.fr custom resources into the
// platform's backends.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	v1alpha1 "github.com/this-is-tobi/ohmlab/utils/api/v1alpha1"
	"github.com/this-is-tobi/ohmlab/utils/internal/buildinfo"
	"github.com/this-is-tobi/ohmlab/utils/internal/controller/config"
	"github.com/this-is-tobi/ohmlab/utils/internal/controller/metrics"
	ctrlruntime "github.com/this-is-tobi/ohmlab/utils/internal/controller/runtime"
	"github.com/this-is-tobi/ohmlab/utils/internal/controller/vaultseed"
)

func main() {
	ctx := ctrl.SetupSignalHandler()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

// run is main without the process: it returns the exit code.
func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("ohmlab-controller", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		configPath  = fs.String("config", "/etc/ohmlab/controller.yaml", "path to the configuration file")
		targets     = fs.String("target", "", "comma-separated targets this process runs: vault")
		metricsAddr = fs.String("metrics-addr", ":8080", "address of the metrics endpoint")
		probeAddr   = fs.String("probe-addr", ":8081", "address of the probe endpoint")
		logLevel    = fs.String("log-level", "info", "log level: debug, info, warn, error")
		version     = fs.Bool("version", false, "print the version and exit")
	)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *version {
		fmt.Fprintf(stdout, "ohmlab-controller %s (commit %s, built %s)\n", buildinfo.Version, buildinfo.Commit, buildinfo.Date)
		return 0
	}

	handler := slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: level(*logLevel)})
	slog.SetDefault(slog.New(handler))
	ctrl.SetLogger(logr.FromSlogHandler(handler))

	if err := serve(ctx, *configPath, splitList(*targets), *metricsAddr, *probeAddr); err != nil {
		fmt.Fprintln(stderr, "ohmlab-controller:", err)
		return 1
	}
	return 0
}

func serve(ctx context.Context, configPath string, targets []string, metricsAddr, probeAddr string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return err
	}
	if err := cfg.Validate(targets); err != nil {
		return fmt.Errorf("config %s: %w", configPath, err)
	}
	resync, _ := cfg.ResyncPeriod()
	leaseNamespace := cfg.LeaseNamespace
	if leaseNamespace == "" {
		if leaseNamespace = os.Getenv("POD_NAMESPACE"); leaseNamespace == "" {
			return fmt.Errorf("leaseNamespace is not set in %s and POD_NAMESPACE is empty", configPath)
		}
	}

	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{clientgoscheme.AddToScheme, apiextensionsv1.AddToScheme, v1alpha1.AddToScheme} {
		if err := add(scheme); err != nil {
			return err
		}
	}

	m := metrics.New(ctrlmetrics.Registry)
	var built []ctrlruntime.Target
	for _, name := range targets {
		switch name {
		case config.TargetVault:
			t, err := vaultseed.NewTarget(cfg.Vault, resync, m)
			if err != nil {
				return err
			}
			built = append(built, t)
		}
	}

	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("kubeconfig: %w", err)
	}
	slog.Info("starting", "targets", targets, "version", buildinfo.Version, "resync", resync.String())
	return ctrlruntime.Run(ctx, restCfg, ctrlruntime.Options{
		Scheme:         scheme,
		LeaseNamespace: leaseNamespace,
		MetricsAddr:    metricsAddr,
		ProbeAddr:      probeAddr,
		Gatherer:       ctrlmetrics.Registry,
		LeaseDuration:  30 * time.Second,
		RenewDeadline:  20 * time.Second,
		RetryPeriod:    5 * time.Second,
	}, built)
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func level(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	}
	return slog.LevelInfo
}
