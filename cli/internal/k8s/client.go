package k8s

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// Client provides a lightweight in-cluster Kubernetes API client.
// Only supports reading secrets — no dependency on client-go.
type Client struct {
	host   string
	token  string
	client *http.Client
}

// NewInClusterClient creates a K8s client using the in-cluster service account.
func NewInClusterClient() (*Client, error) {
	host := os.Getenv("KUBERNETES_SERVICE_HOST")
	port := os.Getenv("KUBERNETES_SERVICE_PORT")
	if host == "" || port == "" {
		return nil, fmt.Errorf("not running in-cluster: KUBERNETES_SERVICE_HOST/PORT not set")
	}

	token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
	if err != nil {
		return nil, fmt.Errorf("read SA token: %w", err)
	}

	caCert, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
	if err != nil {
		return nil, fmt.Errorf("read CA cert: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse cluster CA cert")
	}

	return &Client{
		host:  fmt.Sprintf("https://%s:%s", host, port),
		token: string(token),
		client: &http.Client{
			Timeout: 15 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: pool,
				},
			},
		},
	}, nil
}

// SecretData holds the decoded key-value pairs from a Kubernetes secret.
type SecretData map[string]string

// GetSecret reads a Kubernetes secret and returns its decoded data.
func (c *Client) GetSecret(namespace, name string) (SecretData, error) {
	url := fmt.Sprintf("%s/api/v1/namespaces/%s/secrets/%s", c.host, namespace, name)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP GET %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == 404 {
		return nil, fmt.Errorf("secret %s/%s not found", namespace, name)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("unexpected status %d for secret %s/%s: %s", resp.StatusCode, namespace, name, string(body))
	}

	var secret struct {
		Data map[string]string `json:"data"` // base64-encoded
	}
	if err := json.Unmarshal(body, &secret); err != nil {
		return nil, fmt.Errorf("decode secret response: %w", err)
	}

	result := make(SecretData, len(secret.Data))
	for k, v := range secret.Data {
		decoded, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			return nil, fmt.Errorf("decode secret key %q: %w", k, err)
		}
		result[k] = string(decoded)
	}

	return result, nil
}
