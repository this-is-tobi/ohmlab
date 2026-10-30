package sonarqube

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/this-is-tobi/ohmlab/utils/internal/k8s"
	sonarqubeclient "github.com/this-is-tobi/ohmlab/utils/internal/sonarqube"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Configure SonarQube admin group and permissions",
	Long: `Configure SonarQube admin group and permissions.

Creates an 'admin' group with system-level permissions:
  admin, gateadmin, profileadmin, provisioning, scan

Loads credentials from Kubernetes secrets (when in-cluster) or environment variables.

Environment variables:
  SONARQUBE_DOMAIN, SONARQUBE_USERNAME, SONARQUBE_PASSWORD
  SONARQUBE_NAMESPACE, SONARQUBE_SECRET_NAME`,
	RunE: runConfig,
}

var (
	sonarqubeDomain     string
	sonarqubeUsername   string
	sonarqubePassword   string
	sonarqubeNamespace  string
	sonarqubeSecretName string
)

func init() {
	Cmd.AddCommand(configCmd)

	f := configCmd.Flags()
	f.StringVar(&sonarqubeDomain, "domain", os.Getenv("SONARQUBE_DOMAIN"), "SonarQube domain")
	f.StringVar(&sonarqubeUsername, "username", os.Getenv("SONARQUBE_USERNAME"), "SonarQube admin username")
	f.StringVar(&sonarqubePassword, "password", "", "SonarQube admin password (use env SONARQUBE_PASSWORD)")
	f.StringVar(&sonarqubeNamespace, "namespace", envOrDefault("SONARQUBE_NAMESPACE", "sonarqube"), "SonarQube K8s namespace")
	f.StringVar(&sonarqubeSecretName, "secret", envOrDefault("SONARQUBE_SECRET_NAME", "sonarqube-secret"), "SonarQube K8s secret name")
}

func runConfig(cmd *cobra.Command, args []string) error {
	// Try loading from K8s secrets
	if err := loadFromK8s(); err != nil {
		slog.Debug("could not load from K8s secrets (may be running locally)", "error", err)
	}

	if sonarqubePassword == "" {
		sonarqubePassword = os.Getenv("SONARQUBE_PASSWORD")
	}

	cfg := sonarqubeclient.Config{
		Domain:      sonarqubeDomain,
		Username:    sonarqubeUsername,
		Password:    sonarqubePassword,
		MaxRetries:  5,
		RetryDelay:  10 * time.Second,
		HTTPTimeout: 30 * time.Second,
	}

	if err := sonarqubeclient.Configure(cfg); err != nil {
		return fmt.Errorf("sonarqube configuration failed: %w", err)
	}

	return nil
}

func loadFromK8s() error {
	client, err := k8s.NewInClusterClient()
	if err != nil {
		return err
	}

	secret, err := client.GetSecret(sonarqubeNamespace, sonarqubeSecretName)
	if err != nil {
		return fmt.Errorf("load sonarqube secret: %w", err)
	}

	if sonarqubeDomain == "" {
		sonarqubeDomain = secret["domain"]
	}
	if sonarqubeUsername == "" {
		sonarqubeUsername = secret["admin.username"]
	}
	if sonarqubePassword == "" {
		sonarqubePassword = secret["admin.password"]
	}

	return nil
}
