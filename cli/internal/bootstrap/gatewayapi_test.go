package bootstrap

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func crdYAML(name string) string {
	return "apiVersion: apiextensions.k8s.io/v1\nkind: CustomResourceDefinition\nmetadata:\n  name: " + name + "\n"
}

// serve answers every request with status and body.
func serve(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetchManifestsDecodesEveryObject(t *testing.T) {
	body := "---\n" + crdYAML("gadgets.example.com") + "---\n" + crdYAML("widgets.example.com") + "---\n# a comment only\n"
	objs, err := FetchManifests(context.Background(), http.DefaultClient, serve(t, http.StatusOK, body))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range objs {
		got = append(got, o.GetKind()+"/"+o.GetName())
	}
	if want := "CustomResourceDefinition/gadgets.example.com CustomResourceDefinition/widgets.example.com"; strings.Join(got, " ") != want {
		t.Errorf("got %v, want %s", got, want)
	}
}

func TestFetchManifestsRefusesAnHTTPError(t *testing.T) {
	_, err := FetchManifests(context.Background(), http.DefaultClient, serve(t, http.StatusNotFound, "Not Found"))
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("got %v, want an error carrying the 404", err)
	}
}

func TestFetchManifestsRefusesAnOversizedBody(t *testing.T) {
	body := "#" + strings.Repeat("x", 16<<20)
	if _, err := FetchManifests(context.Background(), http.DefaultClient, serve(t, http.StatusOK, body)); err == nil || !strings.Contains(err.Error(), "16 MiB") {
		t.Errorf("got %v, want an error over the 16 MiB bound", err)
	}
}

func TestFetchManifestsRefusesAnObjectWithoutKind(t *testing.T) {
	body := crdYAML("gadgets.example.com") + "---\nmetadata:\n  name: stray\n"
	if _, err := FetchManifests(context.Background(), http.DefaultClient, serve(t, http.StatusOK, body)); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Errorf("got %v, want an error about the object without a kind", err)
	}
}

func TestGatewayAPIURL(t *testing.T) {
	if got, want := GatewayAPIURL("v1.7.0"), "https://github.com/kubernetes-sigs/gateway-api/releases/download/v1.7.0/experimental-install.yaml"; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}
