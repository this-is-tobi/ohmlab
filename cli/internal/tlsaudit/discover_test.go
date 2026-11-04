package tlsaudit

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func service(ns, name string, labels map[string]string, ports ...corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name, Labels: labels},
		Spec:       corev1.ServiceSpec{ClusterIP: "10.43.0.10", Ports: ports},
	}
}

func port(name string, number int32) corev1.ServicePort {
	return corev1.ServicePort{Name: name, Port: number, Protocol: corev1.ProtocolTCP}
}

func str(s string) *string { return &s }
func i32(n int32) *int32   { return &n }
func boolean(b bool) *bool { return &b }

// slice is the EndpointSlice of a Service: one port, and pods with their readiness.
func slice(ns, svc, portName string, podPort int32, pods map[string]bool) *discoveryv1.EndpointSlice {
	s := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: svc + "-abcde", Labels: map[string]string{discoveryv1.LabelServiceName: svc}},
		Ports:      []discoveryv1.EndpointPort{{Name: str(portName), Port: i32(podPort)}},
	}
	for pod, ready := range pods {
		s.Endpoints = append(s.Endpoints, discoveryv1.Endpoint{
			Conditions: discoveryv1.EndpointConditions{Ready: boolean(ready)},
			TargetRef:  &corev1.ObjectReference{Kind: "Pod", Namespace: ns, Name: pod},
		})
	}
	return s
}

func TestDiscoverResolvesNamedTargetPort(t *testing.T) {
	cs := fake.NewClientset(
		service("app", "web", nil, port("http", 80)),
		slice("app", "web", "http", 8080, map[string]bool{"web-2": true, "web-1": true, "web-0": false}),
	)
	got, err := Discover(context.Background(), cs)
	if err != nil {
		t.Fatal(err)
	}
	want := []Target{{Namespace: "app", Service: "web", Port: 80, PortName: "http", Pod: "web-1", PodPort: 8080}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// A headless Service without a selector has no pods behind it to forward to.
func TestDiscoverSkipsSelectorlessHeadlessExternalNameUDPAndKubernetes(t *testing.T) {
	headless := service("app", "headless", nil, port("http", 80))
	headless.Spec.ClusterIP = corev1.ClusterIPNone
	external := service("app", "external", nil, port("http", 80))
	external.Spec.Type = corev1.ServiceTypeExternalName
	dns := service("kube-system", "kube-dns", nil, port("dns-tcp", 53),
		corev1.ServicePort{Name: "dns", Port: 53, Protocol: corev1.ProtocolUDP})
	cs := fake.NewClientset(headless, external, dns, service("default", "kubernetes", nil, port("https", 443)))
	got, err := Discover(context.Background(), cs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Service != "kube-dns" || got[0].PortName != "dns-tcp" || got[0].Pod != "" {
		t.Errorf("got %+v, want only kube-dns dns-tcp, with no endpoint", got)
	}
}

// A route may lead to a headless Service (Gitea's chart makes gitea-http one);
// one whose pod port a cluster IP already reaches is a duplicate.
func TestDiscoverProbesHeadlessServicesNothingElseReaches(t *testing.T) {
	web := service("app", "web-http", nil, port("http", 3000))
	web.Spec.ClusterIP, web.Spec.Selector = corev1.ClusterIPNone, map[string]string{"app": "web"}
	dup := service("app", "api-headless", nil, port("http", 8080))
	dup.Spec.ClusterIP, dup.Spec.Selector = corev1.ClusterIPNone, map[string]string{"app": "api"}
	cs := fake.NewClientset(web, dup, service("app", "api", nil, port("http", 8080)),
		slice("app", "web-http", "http", 3000, map[string]bool{"web-0": true}),
		slice("app", "api", "http", 8080, map[string]bool{"api-0": true}),
		slice("app", "api-headless", "http", 8080, map[string]bool{"api-0": true}),
	)
	got, err := Discover(context.Background(), cs)
	if err != nil {
		t.Fatal(err)
	}
	var probed []string
	for _, tg := range got {
		probed = append(probed, tg.Service+"/"+tg.Pod)
	}
	if want := []string{"api/api-0", "web-http/web-0"}; !reflect.DeepEqual(probed, want) {
		t.Errorf("probed %v, want %v", probed, want)
	}
}

// A ServiceMonitor scraping a port does not make it a metrics endpoint: UIs and
// APIs serve their own /metrics. Only what is named for metrics counts.
func TestDiscoverMarksPortsNamedForMetrics(t *testing.T) {
	cs := fake.NewClientset(
		service("app", "x", nil, port("http-metrics", 9402), port("http-exporter-port", 9101), port("metrics", 44180), port("http", 80)),
		service("monitoring", "kube-state-metrics", nil, port("http", 8080)),
		service("monitoring", "grafana", nil, port("http-web", 80)),
	)
	got, err := Discover(context.Background(), cs)
	if err != nil {
		t.Fatal(err)
	}
	marked := map[string]bool{}
	for _, tg := range got {
		marked[tg.Service+"/"+tg.PortName] = tg.Metrics
	}
	want := map[string]bool{"x/http-metrics": true, "x/http-exporter-port": true, "x/metrics": true, "x/http": false,
		"kube-state-metrics/http": true, "grafana/http-web": false}
	if !reflect.DeepEqual(marked, want) {
		t.Errorf("metrics marks %v, want %v", marked, want)
	}
}

// Loki's chart names its API port http-metrics: there the name proves nothing.
func TestDiscoverDoesNotTakeLokisAPIForMetrics(t *testing.T) {
	cs := fake.NewClientset(service("monitoring", "loki", map[string]string{"app.kubernetes.io/name": "loki"}, port("http-metrics", 3100)))
	got, err := Discover(context.Background(), cs)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Metrics {
		t.Errorf("got %+v, want Loki's 3100 not marked as metrics", got)
	}
}

func TestWireGuardReadsCiliumConfig(t *testing.T) {
	on := fake.NewClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Namespace: "kube-system", Name: "cilium-config"},
		Data:       map[string]string{"enable-wireguard": "true"},
	})
	if got, err := WireGuard(context.Background(), on); err != nil || !got {
		t.Errorf("with enable-wireguard: got %v, %v; want true", got, err)
	}
	if got, err := WireGuard(context.Background(), fake.NewClientset()); err != nil || got {
		t.Errorf("without cilium-config: got %v, %v; want false and no error", got, err)
	}
}
