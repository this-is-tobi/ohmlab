package tlsaudit

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"

	"k8s.io/apimachinery/pkg/util/httpstream"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"
)

// NewForwarder returns a ForwardFunc that port-forwards through the API
// server, as kubectl port-forward does (WebSocket, falling back to SPDY), on
// a free local port.
func NewForwarder(cfg *rest.Config, cs kubernetes.Interface) (ForwardFunc, error) {
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Transport: transport}
	return func(ctx context.Context, namespace, pod string, port int32) (DialFunc, func(), error) {
		url := cs.CoreV1().RESTClient().Post().Resource("pods").Namespace(namespace).Name(pod).SubResource("portforward").URL()
		dialer := httpstream.Dialer(spdy.NewDialer(upgrader, client, http.MethodPost, url))
		if websocket, err := portforward.NewSPDYOverWebsocketDialer(url, cfg); err == nil {
			dialer = portforward.NewFallbackDialer(websocket, dialer, func(err error) bool {
				return httpstream.IsUpgradeFailure(err) || httpstream.IsHTTPSProxyError(err)
			})
		}
		stop, ready := make(chan struct{}), make(chan struct{})
		fw, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("0:%d", port)}, stop, ready, io.Discard, io.Discard)
		if err != nil {
			return nil, nil, err
		}
		failed := make(chan error, 1)
		go func() { failed <- fw.ForwardPorts() }()
		var once sync.Once
		closeIt := func() { once.Do(func() { close(stop) }) }
		select {
		case <-ready:
		case err := <-failed:
			return nil, nil, err
		case <-ctx.Done():
			closeIt()
			return nil, nil, ctx.Err()
		}
		ports, err := fw.GetPorts()
		if err != nil || len(ports) == 0 {
			closeIt()
			return nil, nil, fmt.Errorf("no local port: %v", err)
		}
		local := fmt.Sprintf("127.0.0.1:%d", ports[0].Local)
		dial := func(ctx context.Context) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "tcp", local)
		}
		return dial, closeIt, nil
	}, nil
}
