package tlsaudit

import (
	"context"
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Target is one Service port and the pod a probe of it goes through.
type Target struct {
	Namespace string `json:"namespace"`
	Service   string `json:"service"`
	Port      int32  `json:"port"`
	PortName  string `json:"portName,omitempty"`
	Pod       string `json:"pod,omitempty"`     // ready endpoint probed; empty: none
	PodPort   int32  `json:"podPort,omitempty"` // resolved target port
	Metrics   bool   `json:"metrics,omitempty"` // the port or its Service is named for metrics
}

// Discover lists every TCP port of every Service, with the first ready pod
// behind it (by name) and the port it maps to there. A headless Service counts
// for the pod ports no cluster IP reaches: a route may lead to one (Gitea's
// chart makes gitea-http headless), and the others repeat a cluster IP's.
func Discover(ctx context.Context, cs kubernetes.Interface) ([]Target, error) {
	services, err := cs.CoreV1().Services("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	endpointSlices, err := cs.DiscoveryV1().EndpointSlices("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list endpoint slices: %w", err)
	}
	byService := map[string][]discoveryv1.EndpointSlice{}
	for _, s := range endpointSlices.Items {
		if name := s.Labels[discoveryv1.LabelServiceName]; name != "" {
			byService[s.Namespace+"/"+name] = append(byService[s.Namespace+"/"+name], s)
		}
	}

	var targets, headless []Target
	for _, svc := range services.Items {
		switch {
		case svc.Spec.Type == corev1.ServiceTypeExternalName,
			svc.Namespace == metav1.NamespaceDefault && svc.Name == "kubernetes",
			svc.Spec.ClusterIP == corev1.ClusterIPNone && len(svc.Spec.Selector) == 0: // no pods behind it
			continue
		}
		for _, p := range svc.Spec.Ports {
			if p.Protocol != "" && p.Protocol != corev1.ProtocolTCP {
				continue
			}
			t := Target{Namespace: svc.Namespace, Service: svc.Name, Port: p.Port, PortName: p.Name}
			t.Pod, t.PodPort = readyEndpoint(byService[svc.Namespace+"/"+svc.Name], p.Name)
			t.Metrics = (metricsName(p.Name) || metricsName(svc.Name)) && !apiOnMetricsPort[svc.Labels["app.kubernetes.io/name"]]
			if svc.Spec.ClusterIP == corev1.ClusterIPNone {
				headless = append(headless, t)
			} else {
				targets = append(targets, t)
			}
		}
	}
	reached := map[string]bool{}
	for _, t := range targets {
		reached[podAddress(t)] = true
	}
	for _, t := range headless {
		if t.Pod == "" || !reached[podAddress(t)] {
			targets = append(targets, t)
		}
	}
	sortTargets(targets)
	return targets, nil
}

func podAddress(t Target) string {
	return fmt.Sprintf("%s/%s:%d", t.Namespace, t.Pod, t.PodPort)
}

// readyEndpoint returns the first ready pod by name and its port for the
// Service port named portName ("" for an unnamed single port).
func readyEndpoint(endpointSlices []discoveryv1.EndpointSlice, portName string) (string, int32) {
	var pod string
	var podPort int32
	for _, s := range endpointSlices {
		var number *int32
		for _, p := range s.Ports {
			if p.Name != nil && *p.Name == portName || p.Name == nil && portName == "" {
				number = p.Port
				break
			}
		}
		if number == nil {
			continue
		}
		for _, e := range s.Endpoints {
			if e.TargetRef == nil || e.TargetRef.Kind != "Pod" || e.Conditions.Ready != nil && !*e.Conditions.Ready {
				continue
			}
			if pod == "" || e.TargetRef.Name < pod {
				pod, podPort = e.TargetRef.Name, *number
			}
		}
	}
	return pod, podPort
}

// The apps (by app.kubernetes.io/name) whose ports named for metrics serve
// their API too: Loki's http-metrics port takes the push and query API.
var apiOnMetricsPort = map[string]bool{"loki": true}

// metricsName tells a name given to metrics. The rule lets metrics endpoints
// stay plaintext; a port a ServiceMonitor scrapes is not one by that alone, as
// UIs and APIs serve their own /metrics.
func metricsName(name string) bool {
	n := strings.ToLower(name)
	return strings.Contains(n, "metrics") || strings.Contains(n, "exporter")
}

// WireGuard tells whether Cilium encrypts the traffic between nodes.
func WireGuard(ctx context.Context, cs kubernetes.Interface) (bool, error) {
	cm, err := cs.CoreV1().ConfigMaps("kube-system").Get(ctx, "cilium-config", metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read kube-system/cilium-config: %w", err)
	}
	return cm.Data["enable-wireguard"] == "true", nil
}

func sortTargets(targets []Target) {
	sort.Slice(targets, func(i, j int) bool { return less(targets[i], targets[j]) })
}

// less orders targets by namespace, Service and port.
func less(a, b Target) bool {
	if a.Namespace != b.Namespace {
		return a.Namespace < b.Namespace
	}
	if a.Service != b.Service {
		return a.Service < b.Service
	}
	return a.Port < b.Port
}
