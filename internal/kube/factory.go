package kube

import (
	"fmt"

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

// RESTConfig resolves a kubeconfig context into connection settings.
//
// clientcmd is used rather than reading the YAML directly, so exec plugins,
// auth providers, proxy settings and certificate references behave exactly as
// they do for kubectl.
func (f *Factory) RESTConfig(kubeconfigPath, contextName string) (*rest.Config, error) {
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

	restConfig.UserAgent = f.UserAgent
	restConfig.Timeout = RequestTimeout

	// Desktop use is bursty: opening a cluster lists a dozen resource types at
	// once. The client-go defaults (5 QPS) would queue those behind each other
	// and make the first screen feel broken.
	restConfig.QPS = 50
	restConfig.Burst = 100

	return restConfig, nil
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
	restConfig, err := f.RESTConfig(kubeconfigPath, contextName)
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
