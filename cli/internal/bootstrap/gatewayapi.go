package bootstrap

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
)

// GatewayAPIVersion is the Gateway API release whose experimental channel a
// bootstrap installs when it is missing: TLSRoute (Teleport's passthrough)
// and Traefik's experimentalChannel need that channel. Renovate bumps it
// together with run.sh's GATEWAY_API_VERSION.
const GatewayAPIVersion = "v1.7.0"

// GatewayAPIMarker is a CRD of the experimental channel alone: when it is
// present, the channel is installed.
const GatewayAPIMarker = "tlsroutes.gateway.networking.k8s.io"

// maxManifests bounds what FetchManifests reads.
const maxManifests = 16 << 20

// GatewayAPIURL is where a Gateway API release publishes its experimental
// channel.
func GatewayAPIURL(version string) string {
	return "https://github.com/kubernetes-sigs/gateway-api/releases/download/" + version + "/experimental-install.yaml"
}

// FetchManifests downloads a multi-document YAML file and decodes its objects.
func FetchManifests(ctx context.Context, client *http.Client, url string) ([]*unstructured.Unstructured, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("fetch %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxManifests+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", url, err)
	}
	if len(data) > maxManifests {
		return nil, fmt.Errorf("fetch %s: more than %d MiB", url, maxManifests>>20)
	}

	var objs []*unstructured.Unstructured
	docs := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
	for {
		doc, err := docs.Read()
		if errors.Is(err, io.EOF) {
			return objs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", url, err)
		}
		js, err := utilyaml.ToJSON(doc)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", url, err)
		}
		if s := string(bytes.TrimSpace(js)); s == "" || s == "null" {
			continue
		}
		var head struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
		}
		if err := json.Unmarshal(js, &head); err != nil {
			return nil, fmt.Errorf("%s: %w", url, err)
		}
		if head.APIVersion == "" || head.Kind == "" {
			return nil, fmt.Errorf("%s: object %d has no apiVersion or kind", url, len(objs)+1)
		}
		obj := &unstructured.Unstructured{}
		if err := obj.UnmarshalJSON(js); err != nil {
			return nil, fmt.Errorf("%s: %w", url, err)
		}
		objs = append(objs, obj)
	}
}
