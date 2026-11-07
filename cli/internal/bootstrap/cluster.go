package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/this-is-tobi/ohmlab/cli/internal/kubeclient"
)

// Cluster is what a bootstrap reads and changes in the cluster itself; Helm
// does the rest.
type Cluster interface {
	HasCRD(ctx context.Context, name string) (bool, error)
	// Apply applies objs server-side as the ohmlab field manager, without
	// forcing: a field another manager owns is a conflict, not a takeover.
	Apply(ctx context.Context, objs []*unstructured.Unstructured) error
	// WaitEstablished waits until every CRD named is Established.
	WaitEstablished(ctx context.Context, names []string, timeout time.Duration) error
	// InitialAdminPassword reads the password Argo CD generated, or
	// ErrNoInitialPassword when its Secret does not exist.
	InitialAdminPassword(ctx context.Context, namespace string) (string, error)
	// CurrentAdminPassword reads the admin password argocd-secret holds, zero
	// when there is none.
	CurrentAdminPassword(ctx context.Context, namespace string) (AdminHash, error)
}

// ErrNoInitialPassword is a namespace without argocd-initial-admin-secret.
var ErrNoInitialPassword = errors.New("no argocd-initial-admin-secret")

const (
	fieldManager  = "ohmlab"
	initialSecret = "argocd-initial-admin-secret"
	argoCDSecret  = "argocd-secret"
)

type kubeCluster struct {
	crds   apiextensionsclient.Interface
	dyn    dynamic.Interface
	mapper meta.RESTMapper
	core   kubernetes.Interface
}

// NewCluster returns the Cluster cfg reaches.
func NewCluster(cfg *rest.Config) (Cluster, error) {
	crds, err := apiextensionsclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	core, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(core.Discovery()))
	return &kubeCluster{crds: crds, dyn: dyn, mapper: mapper, core: core}, nil
}

// Connect returns the Cluster of a kubeconfig context, loaded as kubectl
// does, and its API server.
func Connect(kubeconfig, context string) (Cluster, string, error) {
	// client-go words a missing context as it likes (and IsContextNotFound
	// misses some of its wordings): look for it first.
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	rules.ExplicitPath = kubeconfig
	raw, err := rules.Load()
	if err != nil {
		return nil, "", fmt.Errorf("read kubeconfig: %w", err)
	}
	if _, ok := raw.Contexts[context]; !ok {
		return nil, "", fmt.Errorf("no context %q in the kubeconfig: fetch it with ohmlab kubeconfig, set contextName in ohmlab.yaml, or pass --context", context)
	}
	cfg, _, err := kubeclient.Load(kubeconfig, context)
	if err != nil {
		return nil, "", err
	}
	c, err := NewCluster(cfg)
	if err != nil {
		return nil, "", err
	}
	return c, cfg.Host, nil
}

func (c *kubeCluster) HasCRD(ctx context.Context, name string) (bool, error) {
	_, err := c.crds.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, name, metav1.GetOptions{})
	switch {
	case err == nil:
		return true, nil
	case apierrors.IsNotFound(err):
		return false, nil
	}
	return false, fmt.Errorf("read CRD %s: %w", name, err)
}

func (c *kubeCluster) Apply(ctx context.Context, objs []*unstructured.Unstructured) error {
	for _, o := range objs {
		gvk := o.GroupVersionKind()
		m, err := c.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return fmt.Errorf("apply %s %s: %w", gvk.Kind, o.GetName(), err)
		}
		var res dynamic.ResourceInterface = c.dyn.Resource(m.Resource)
		if m.Scope.Name() == meta.RESTScopeNameNamespace {
			res = c.dyn.Resource(m.Resource).Namespace(o.GetNamespace())
		}
		if _, err := res.Apply(ctx, o.GetName(), o, metav1.ApplyOptions{FieldManager: fieldManager}); err != nil {
			return fmt.Errorf("apply %s %s: %w", gvk.Kind, o.GetName(), err)
		}
	}
	return nil
}

func (c *kubeCluster) WaitEstablished(ctx context.Context, names []string, timeout time.Duration) error {
	pending := slices.Clone(names)
	err := wait.PollUntilContextTimeout(ctx, 500*time.Millisecond, timeout, true, func(ctx context.Context) (bool, error) {
		left := pending[:0]
		for _, n := range pending {
			crd, err := c.crds.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, n, metav1.GetOptions{})
			if err != nil && !apierrors.IsNotFound(err) {
				return false, fmt.Errorf("read CRD %s: %w", n, err)
			}
			if err != nil || !established(crd) {
				left = append(left, n)
			}
		}
		pending = left
		return len(pending) == 0, nil
	})
	if err != nil && len(pending) > 0 && ctx.Err() == nil {
		return fmt.Errorf("not Established after %s: %s", timeout, strings.Join(pending, ", "))
	}
	return err
}

func established(crd *apiextensionsv1.CustomResourceDefinition) bool {
	return slices.ContainsFunc(crd.Status.Conditions, func(c apiextensionsv1.CustomResourceDefinitionCondition) bool {
		return c.Type == apiextensionsv1.Established && c.Status == apiextensionsv1.ConditionTrue
	})
}

func (c *kubeCluster) InitialAdminPassword(ctx context.Context, namespace string) (string, error) {
	s, err := c.core.CoreV1().Secrets(namespace).Get(ctx, initialSecret, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return "", fmt.Errorf("%w in %s", ErrNoInitialPassword, namespace)
	}
	if err != nil {
		return "", fmt.Errorf("read %s/%s: %w", namespace, initialSecret, err)
	}
	pw := string(s.Data["password"])
	if pw == "" {
		return "", fmt.Errorf("%s/%s holds no password", namespace, initialSecret)
	}
	return pw, nil
}

func (c *kubeCluster) CurrentAdminPassword(ctx context.Context, namespace string) (AdminHash, error) {
	s, err := c.core.CoreV1().Secrets(namespace).Get(ctx, argoCDSecret, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return AdminHash{}, nil
	}
	if err != nil {
		return AdminHash{}, fmt.Errorf("read %s/%s: %w", namespace, argoCDSecret, err)
	}
	return AdminHash{Hash: string(s.Data["admin.password"]), Mtime: string(s.Data["admin.passwordMtime"])}, nil
}
