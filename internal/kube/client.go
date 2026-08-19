// Package kube owns everything that speaks to a Kubernetes API server.
//
// Nothing above this package imports client-go, and nothing in it knows about
// Wails, customers or VPNs. Kubernetes access is reached exclusively through
// client-go: no hand-rolled HTTP against the API, no assumptions about URL
// shapes that change between releases.
package kube

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// ClusterClient is the set of clients one cluster session uses.
//
// Every cluster gets its own instance. There is no package-level client:
// working two customers at once with a shared client is how an engineer runs a
// command against the wrong production cluster.
type ClusterClient struct {
	RESTConfig *rest.Config
	Clientset  kubernetes.Interface
	Dynamic    dynamic.Interface
	Discovery  discovery.DiscoveryInterface

	// StreamClientset and StreamDynamic are the same clients with no
	// client-side request timeout, for calls whose response body is meant to
	// stay open indefinitely: followed logs and watches.
	//
	// http.Client.Timeout bounds the whole exchange including reading the
	// body, not just the wait for response headers, so RequestTimeout would
	// cut a log follow off mid-stream and report it as "request canceled
	// (Client.Timeout or context cancellation while reading body)". These
	// calls are bounded by their context instead, which is cancelled when the
	// viewer closes or the cluster disconnects.
	StreamClientset kubernetes.Interface
	StreamDynamic   dynamic.Interface

	// Metrics is nil when the cluster has no metrics-server. Callers must
	// degrade instead of failing: most on-premise clusters do not have one.
	Metrics metricsv.Interface

	// ServerVersion is filled once at connect time and shown in the header.
	ServerVersion string
}

// Timeouts sized for a human waiting at a desktop window, not for a controller
// reconciling in a loop.
const (
	// ConnectTimeout bounds the first contact with an API server. Behind a VPN
	// an unreachable endpoint hangs until the TCP stack gives up, which is far
	// longer than a person will sit and watch a spinner.
	ConnectTimeout = 10 * time.Second

	// RequestTimeout bounds ordinary list and get calls. It is deliberately
	// absent from the streaming clients, where a bound on the response body
	// would end a log follow or a watch that is behaving correctly.
	RequestTimeout = 30 * time.Second
)

// Close releases anything the clients hold. client-go's clients are stateless
// over HTTP, so this exists for symmetry with future transports.
func (c *ClusterClient) Close() {}

// FetchServerVersion asks the API server what it is, which doubles as the
// first authenticated round trip of a connection attempt.
func (c *ClusterClient) FetchServerVersion(ctx context.Context) (string, error) {
	type result struct {
		version string
		err     error
	}
	done := make(chan result, 1)

	// The discovery client does not take a context, so the call is bounded by
	// running it alongside one rather than by trusting it to return.
	go func() {
		info, err := c.Discovery.ServerVersion()
		if err != nil {
			done <- result{err: err}
			return
		}
		done <- result{version: info.GitVersion}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case r := <-done:
		if r.err != nil {
			return "", fmt.Errorf("read server version: %w", r.err)
		}
		return r.version, nil
	}
}
