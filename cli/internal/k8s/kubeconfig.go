package k8s

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// kubeconfig models the subset of a kubeconfig file needed to build a client:
// static certs or tokens only (no exec/auth-provider plugins — K3s and most
// homelab distros emit static credentials).
type kubeconfig struct {
	CurrentContext string `yaml:"current-context"`
	Clusters       []struct {
		Name    string `yaml:"name"`
		Cluster struct {
			Server                   string `yaml:"server"`
			CertificateAuthorityData string `yaml:"certificate-authority-data"`
			CertificateAuthority     string `yaml:"certificate-authority"`
			InsecureSkipTLSVerify    bool   `yaml:"insecure-skip-tls-verify"`
		} `yaml:"cluster"`
	} `yaml:"clusters"`
	Contexts []struct {
		Name    string `yaml:"name"`
		Context struct {
			Cluster string `yaml:"cluster"`
			User    string `yaml:"user"`
		} `yaml:"context"`
	} `yaml:"contexts"`
	Users []struct {
		Name string `yaml:"name"`
		User struct {
			ClientCertificateData string `yaml:"client-certificate-data"`
			ClientKeyData         string `yaml:"client-key-data"`
			ClientCertificate     string `yaml:"client-certificate"`
			ClientKey             string `yaml:"client-key"`
			Token                 string `yaml:"token"`
		} `yaml:"user"`
	} `yaml:"users"`
}

// NewClient builds a Kubernetes API client from the in-cluster service
// account when available, falling back to $KUBECONFIG / ~/.kube/config.
func NewClient() (*Client, error) {
	if c, err := NewInClusterClient(); err == nil {
		return c, nil
	}
	return NewKubeconfigClient("")
}

// NewKubeconfigClient builds a client from a kubeconfig file (current-context).
// An empty path resolves $KUBECONFIG, then ~/.kube/config.
func NewKubeconfigClient(path string) (*Client, error) {
	if path == "" {
		path = os.Getenv("KUBECONFIG")
	}
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home dir: %w", err)
		}
		path = filepath.Join(home, ".kube", "config")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig: %w", err)
	}
	var cfg kubeconfig
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}
	if cfg.CurrentContext == "" {
		return nil, fmt.Errorf("kubeconfig has no current-context")
	}

	var clusterName, userName string
	for _, c := range cfg.Contexts {
		if c.Name == cfg.CurrentContext {
			clusterName, userName = c.Context.Cluster, c.Context.User
		}
	}
	if clusterName == "" {
		return nil, fmt.Errorf("context %q not found in kubeconfig", cfg.CurrentContext)
	}

	tlsCfg := &tls.Config{}
	var server string
	for _, c := range cfg.Clusters {
		if c.Name != clusterName {
			continue
		}
		server = c.Cluster.Server
		tlsCfg.InsecureSkipVerify = c.Cluster.InsecureSkipTLSVerify
		caPEM, err := readInlineOrFile(c.Cluster.CertificateAuthorityData, c.Cluster.CertificateAuthority)
		if err != nil {
			return nil, fmt.Errorf("cluster CA: %w", err)
		}
		if len(caPEM) > 0 {
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caPEM) {
				return nil, fmt.Errorf("failed to parse cluster CA cert")
			}
			tlsCfg.RootCAs = pool
		}
	}
	if server == "" {
		return nil, fmt.Errorf("cluster %q not found in kubeconfig", clusterName)
	}

	var token string
	for _, u := range cfg.Users {
		if u.Name != userName {
			continue
		}
		token = u.User.Token
		certPEM, err := readInlineOrFile(u.User.ClientCertificateData, u.User.ClientCertificate)
		if err != nil {
			return nil, fmt.Errorf("client cert: %w", err)
		}
		keyPEM, err := readInlineOrFile(u.User.ClientKeyData, u.User.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("client key: %w", err)
		}
		if len(certPEM) > 0 && len(keyPEM) > 0 {
			cert, err := tls.X509KeyPair(certPEM, keyPEM)
			if err != nil {
				return nil, fmt.Errorf("load client keypair: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
	}

	return &Client{
		host:  server,
		token: token,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}, nil
}

// readInlineOrFile decodes base64 inline data, or reads the referenced file.
func readInlineOrFile(inline, file string) ([]byte, error) {
	if inline != "" {
		return base64.StdEncoding.DecodeString(inline)
	}
	if file != "" {
		return os.ReadFile(file)
	}
	return nil, nil
}
