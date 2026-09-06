package kube

import (
	"fmt"
	"net/url"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	metricsv "k8s.io/metrics/pkg/client/clientset/versioned"
)

// Factory builds a client set for one kubeconfig context.
type Factory struct {
	// UserAgent identifies Biebie Kube in API server audit logs, so a cluster
	// owner can see which tool made a change.
	UserAgent string
}

// NewFactory creates a factory with the standard user agent.
func NewFactory(version string) *Factory {
	return &Factory{UserAgent: "biebie-kube/" + version}
}

// Endpoint redirects a connection to where the API server can actually be
// reached, when that is not the address its kubeconfig names.
//
// This is the shape of an SSH tunnel: the cluster is at 172.16.20.65:6443 and
// always will be, but the only way to it is a local port that Biebie Access is
// lending. The kubeconfig is never rewritten — it stays the record of what the
// cluster is — and the substitution lives for exactly one session.
type Endpoint struct {
	// Address is the host:port to dial instead, e.g. "127.0.0.1:16443".
	Address string

	// ServerName is the name the API server's certificate was issued for.
	//
	// It is the load-bearing half of this struct. A certificate issued for the
	// cluster's own address does not match 127.0.0.1, so without this the
	// handshake fails and the obvious next move is to disable verification —
	// which would hand the cluster's traffic to anything answering on a local
	// port. Naming the original host instead keeps verification fully intact.
	ServerName string
}

// RESTConfig resolves a kubeconfig context into connection settings.
//
// clientcmd is used rather than reading the YAML directly, so exec plugins,
// auth providers, proxy settings and certificate references behave exactly as
// they do for kubectl.
func (f *Factory) RESTConfig(kubeconfigPath, contextName string) (*rest.Config, error) {
	return f.RESTConfigVia(kubeconfigPath, contextName, nil)
}

// RESTConfigVia resolves a kubeconfig context and, when via is set, points the
// result at the local address standing in for the API server.
func (f *Factory) RESTConfigVia(kubeconfigPath, contextName string, via *Endpoint) (*rest.Config, error) {
	rules := &clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfigPath}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: contextName}

	// NonInteractive matters: a kubeconfig may reference an exec plugin that
	// would otherwise try to prompt on a terminal this application does not
	// have, and hang the connection attempt forever.
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides)

	restConfig, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("read context %q: %w", contextName, err)
	}

	if err := redirect(restConfig, via); err != nil {
		return nil, err
	}

	restConfig.UserAgent = f.UserAgent
	restConfig.Timeout = RequestTimeout

	// Desktop use is bursty: opening a cluster lists a dozen resource types at
	// once. The client-go defaults (5 QPS) would queue those behind each other
	// and make the first screen feel broken.
	restConfig.QPS = 50
	restConfig.Burst = 100

	return restConfig, nil
}

// redirect points a resolved config at a stand-in address without loosening
// anything else about it.
func redirect(restConfig *rest.Config, via *Endpoint) error {
	if via == nil || via.Address == "" {
		return nil
	}

	original, err := url.Parse(restConfig.Host)
	if err != nil {
		return fmt.Errorf("read the API endpoint %q: %w", restConfig.Host, err)
	}
	scheme := original.Scheme
	if scheme == "" {
		scheme = "https"
	}
	restConfig.Host = scheme + "://" + via.Address

	// A kubeconfig that already sets tls-server-name was written by someone who
	// knew what the certificate says, and it is not this code's place to
	// disagree with them.
	if restConfig.TLSClientConfig.ServerName == "" && via.ServerName != "" {
		restConfig.TLSClientConfig.ServerName = via.ServerName
	}
	return nil
}

// StreamConfig copies connection settings for calls that hold their response
// body open, dropping the request timeout. See ClusterClient.StreamClientset
// for why that timeout cannot apply to a stream.
func StreamConfig(config *rest.Config) *rest.Config {
	stream := rest.CopyConfig(config)
	stream.Timeout = 0
	return stream
}

// Build creates every client a session needs.
func (f *Factory) Build(kubeconfigPath, contextName string) (*ClusterClient, error) {
	return f.BuildVia(kubeconfigPath, contextName, nil)
}

// BuildVia creates every client a session needs, reaching the API server
// through a stand-in address when one is given.
func (f *Factory) BuildVia(kubeconfigPath, contextName string, via *Endpoint) (*ClusterClient, error) {
	restConfig, err := f.RESTConfigVia(kubeconfigPath, contextName, via)
	if err != nil {
		return nil, err
	}
	return f.BuildFrom(restConfig)
}

// BuildFrom creates clients from already-resolved settings.
func (f *Factory) BuildFrom(restConfig *rest.Config) (*ClusterClient, error) {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create Kubernetes client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}
	disco, err := discovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create discovery client: %w", err)
	}

	// Streams get their own clients rather than the shared config losing its
	// timeout, so an ordinary list keeps a bound that a person will wait out.
	streamConfig := StreamConfig(restConfig)

	streamClientset, err := kubernetes.NewForConfig(streamConfig)
	if err != nil {
		return nil, fmt.Errorf("create streaming Kubernetes client: %w", err)
	}
	streamDyn, err := dynamic.NewForConfig(streamConfig)
	if err != nil {
		return nil, fmt.Errorf("create streaming dynamic client: %w", err)
	}

	client := &ClusterClient{
		RESTConfig:      restConfig,
		Clientset:       clientset,
		Dynamic:         dyn,
		Discovery:       disco,
		StreamClientset: streamClientset,
		StreamDynamic:   streamDyn,
	}

	// A cluster without metrics-server is normal, so a failure to construct
	// this client must not fail the connection.
	if metrics, err := metricsv.NewForConfig(restConfig); err == nil {
		client.Metrics = metrics
	}

	return client, nil
}
