package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

// restCfg is envtest's API server, nil without KUBEBUILDER_ASSETS: then the
// tests that need it skip, unless OHMLAB_REQUIRE_ENVTEST demands it.
var restCfg *rest.Config

func TestMain(m *testing.M) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		if os.Getenv("OHMLAB_REQUIRE_ENVTEST") != "" {
			fmt.Fprintln(os.Stderr, "KUBEBUILDER_ASSETS is not set and OHMLAB_REQUIRE_ENVTEST demands an API server")
			os.Exit(1)
		}
		os.Exit(m.Run())
	}
	env := &envtest.Environment{}
	var err error
	if restCfg, err = env.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "start envtest:", err)
		os.Exit(1)
	}
	code := m.Run()
	_ = env.Stop()
	os.Exit(code)
}

// cluster is a Cluster on envtest's API server.
func cluster(t *testing.T) Cluster {
	t.Helper()
	if restCfg == nil {
		t.Skip("KUBEBUILDER_ASSETS is not set: no API server")
	}
	c, err := NewCluster(restCfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// crd is a minimal cluster-scoped CRD of group example.com.
func crd(plural, kind string, shortNames ...string) *apiextensionsv1.CustomResourceDefinition {
	preserve := true
	return &apiextensionsv1.CustomResourceDefinition{
		TypeMeta:   metav1.TypeMeta{APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition"},
		ObjectMeta: metav1.ObjectMeta{Name: plural + ".example.com"},
		Spec: apiextensionsv1.CustomResourceDefinitionSpec{
			Group: "example.com",
			Names: apiextensionsv1.CustomResourceDefinitionNames{Plural: plural, Kind: kind, ShortNames: shortNames},
			Scope: apiextensionsv1.ClusterScoped,
			Versions: []apiextensionsv1.CustomResourceDefinitionVersion{{Name: "v1", Served: true, Storage: true,
				Schema: &apiextensionsv1.CustomResourceValidation{OpenAPIV3Schema: &apiextensionsv1.JSONSchemaProps{
					Type: "object", XPreserveUnknownFields: &preserve}}}},
		},
	}
}

func unstructuredOf(t *testing.T, objs ...runtime.Object) []*unstructured.Unstructured {
	t.Helper()
	var out []*unstructured.Unstructured
	for _, o := range objs {
		m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(o)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, &unstructured.Unstructured{Object: m})
	}
	return out
}

func TestApplyInstallsAndEstablishesCRDs(t *testing.T) {
	c, ctx := cluster(t), context.Background()
	if err := c.Apply(ctx, unstructuredOf(t, crd("gadgets", "Gadget"), crd("widgets", "Widget"))); err != nil {
		t.Fatal(err)
	}
	names := []string{"gadgets.example.com", "widgets.example.com"}
	for _, n := range names {
		if ok, err := c.HasCRD(ctx, n); err != nil || !ok {
			t.Errorf("HasCRD(%s) = %v, %v after Apply", n, ok, err)
		}
	}
	if err := c.WaitEstablished(ctx, names, 30*time.Second); err != nil {
		t.Fatal(err)
	}
	cs := apiextensionsclient.NewForConfigOrDie(restCfg)
	got, err := cs.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, names[0], metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(got.ManagedFields, func(f metav1.ManagedFieldsEntry) bool {
		return f.Manager == "ohmlab" && f.Operation == metav1.ManagedFieldsOperationApply
	}) {
		t.Errorf("managed fields %+v, want an Apply by ohmlab", got.ManagedFields)
	}
}

func TestApplyTwiceIsANoOp(t *testing.T) {
	c, ctx := cluster(t), context.Background()
	objs := unstructuredOf(t, crd("doohickeys", "Doohickey"))
	for i := range 2 {
		if err := c.Apply(ctx, objs); err != nil {
			t.Fatalf("Apply #%d: %v", i+1, err)
		}
	}
}

func TestApplyReportsAConflictAndForcesNothing(t *testing.T) {
	c, ctx := cluster(t), context.Background()
	cs := apiextensionsclient.NewForConfigOrDie(restCfg).ApiextensionsV1().CustomResourceDefinitions()
	if _, err := cs.Create(ctx, crd("sprockets", "Sprocket", "sp"), metav1.CreateOptions{FieldManager: "helm"}); err != nil {
		t.Fatal(err)
	}
	err := c.Apply(ctx, unstructuredOf(t, crd("sprockets", "Sprocket", "spr")))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "conflict") || !strings.Contains(err.Error(), "sprockets.example.com") {
		t.Errorf("got %v, want a conflict naming sprockets.example.com", err)
	}
	got, err := cs.Get(ctx, "sprockets.example.com", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Spec.Names.ShortNames, []string{"sp"}) {
		t.Errorf("shortNames %v, want helm's [sp] kept", got.Spec.Names.ShortNames)
	}
}

func TestWaitEstablishedTimesOut(t *testing.T) {
	err := cluster(t).WaitEstablished(context.Background(), []string{"nothings.example.com"}, time.Second)
	if err == nil || !strings.Contains(err.Error(), "nothings.example.com") {
		t.Errorf("got %v, want a timeout naming nothings.example.com", err)
	}
}

func TestInitialAdminPassword(t *testing.T) {
	c, ctx := cluster(t), context.Background()
	if _, err := c.InitialAdminPassword(ctx, "argocd-system"); !errors.Is(err, ErrNoInitialPassword) {
		t.Errorf("got %v, want ErrNoInitialPassword without the Secret", err)
	}
	core := kubernetes.NewForConfigOrDie(restCfg).CoreV1()
	if _, err := core.Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "argocd-system"}}, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "argocd-initial-admin-secret"}, Data: map[string][]byte{"password": []byte("pw-123")}}
	if _, err := core.Secrets("argocd-system").Create(ctx, secret, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if got, err := c.InitialAdminPassword(ctx, "argocd-system"); err != nil || got != "pw-123" {
		t.Errorf("got %q, %v; want the Secret's password", got, err)
	}
}

func TestConnectNamesTheMissingContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config")
	write(t, path, `apiVersion: v1
kind: Config
current-context: a
clusters:
- name: a
  cluster: {server: "https://a.example:6443"}
contexts:
- name: a
  context: {cluster: a, user: u}
users:
- name: u
  user: {token: t}
`)
	_, _, err := Connect(path, "lab")
	for _, want := range []string{`no context "lab" in the kubeconfig`, "ohmlab kubeconfig", "contextName", "--context"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("got %v, want an error with %q", err, want)
		}
	}
	if _, server, err := Connect(path, "a"); err != nil || server != "https://a.example:6443" {
		t.Errorf("got %q, %v; want the context's server", server, err)
	}
}

// A context named on purpose wins over the current one and over $KUBECONFIG:
// the current context of a workstation can be any cluster.
func TestConnectTakesTheNamedContextNotTheCurrentOne(t *testing.T) {
	dir := t.TempDir()
	decoy := `apiVersion: v1
kind: Config
current-context: decoy
clusters:
- name: decoy
  cluster: {server: "https://decoy.example:6443"}
contexts:
- name: decoy
  context: {cluster: decoy, user: u}
users:
- name: u
  user: {token: t}
`
	write(t, filepath.Join(dir, "env"), decoy)
	t.Setenv("KUBECONFIG", filepath.Join(dir, "env"))
	path := filepath.Join(dir, "given")
	write(t, path, strings.Replace(strings.Replace(decoy, "contexts:\n", "contexts:\n- name: lab\n  context: {cluster: lab, user: u}\n", 1),
		"clusters:\n", "clusters:\n- name: lab\n  cluster: {server: \"https://lab.example:6443\"}\n", 1))
	if _, server, err := Connect(path, "lab"); err != nil || server != "https://lab.example:6443" {
		t.Errorf("got %q, %v; want lab's server, not the current context's or $KUBECONFIG's", server, err)
	}
}
