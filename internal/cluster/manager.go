package cluster

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"

	bctx "biebie.net/protocol/context"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// AccessChecker is how the lifecycle asks Biebie Access whether a customer
// network is up.
//
// It is an interface because Biebie Kube must run with Biebie Access absent,
// stopped, or replaced by a test double — and because Biebie Kube must never
// contain VPN logic of its own.
type AccessChecker interface {
	// Installed reports whether Biebie Access is reachable at all.
	Installed(ctx context.Context) bool

	// Status reports one profile's connectivity. It never returns credentials.
	Status(ctx context.Context, profileID string) (bctx.AccessStatus, error)
}

// KubeconfigResolver turns a stored reference into a path on disk.
type KubeconfigResolver interface {
	PathFor(ref string) (string, error)
}

// Emitter publishes state changes to the UI. The manager depends on this
// narrow interface rather than on the Wails runtime, so the lifecycle can be
// tested headlessly.
type Emitter interface {
	Emit(event string, data any)
}

// Events published by the manager.
const (
	EventSessionChanged   = "cluster:session"
	EventResourcesChanged = "cluster:resources"
)

// ResourceChange tells the frontend which table went stale.
type ResourceChange struct {
	ClusterID string `json:"clusterId"`
	Resource  string `json:"resource"`
	Group     string `json:"group"`
	Namespace string `json:"namespace"`
}

// session is one cluster's live state.
type session struct {
	cluster domain.Cluster

	state     domain.ClusterState
	namespace string

	client *kube.ClusterClient
	hub    *kube.WatchHub

	serverVersion string
	connectedAt   *time.Time

	namespaces []string
	resources  []kube.APIResource

	diagnosis *domain.Diagnosis
	lastError string
}

// Manager owns cluster sessions: at most one per cluster, each with its own
// clients, watches and namespace.
type Manager struct {
	repo    *Repository
	configs KubeconfigResolver
	factory *kube.Factory
	access  AccessChecker
	emitter Emitter

	mu       sync.RWMutex
	sessions map[string]*session

	// connecting guards against two connect attempts racing for one cluster,
	// which double-clicking Connect would otherwise cause.
	connecting map[string]struct{}
}

// NewManager wires the lifecycle.
func NewManager(
	repo *Repository,
	configs KubeconfigResolver,
	factory *kube.Factory,
	access AccessChecker,
	emitter Emitter,
) *Manager {
	return &Manager{
		repo:       repo,
		configs:    configs,
		factory:    factory,
		access:     access,
		emitter:    emitter,
		sessions:   make(map[string]*session),
		connecting: make(map[string]struct{}),
	}
}

// Repository exposes cluster storage for the binding layer's CRUD calls,
// which have no reason to reach around the manager to reach it.
func (m *Manager) Repository() *Repository { return m.repo }

// Cluster returns a stored cluster record.
func (m *Manager) Cluster(clusterID string) (domain.Cluster, error) { return m.repo.Get(clusterID) }

// Clusters returns every stored cluster.
func (m *Manager) Clusters() []domain.Cluster { return m.repo.All() }

// Session reports a cluster's current state, including one that has never been
// connected.
func (m *Manager) Session(clusterID string) domain.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.sessionView(clusterID)
}

// Sessions reports every cluster's state, for the sidebar.
func (m *Manager) Sessions() []domain.Session {
	clusters := m.repo.All()

	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]domain.Session, 0, len(clusters))
	for _, cluster := range clusters {
		out = append(out, m.sessionView(cluster.ID))
	}
	return out
}

func (m *Manager) sessionView(clusterID string) domain.Session {
	s, ok := m.sessions[clusterID]
	if !ok {
		return domain.Session{ClusterID: clusterID, State: domain.ClusterDisconnected}
	}
	return domain.Session{
		ClusterID:     clusterID,
		State:         s.state,
		Namespace:     s.namespace,
		ServerVersion: s.serverVersion,
		ConnectedAt:   s.connectedAt,
		Diagnosis:     s.diagnosis,
		Error:         s.lastError,
	}
}

// Client returns the clients for a connected cluster.
func (m *Manager) Client(clusterID string) (*kube.ClusterClient, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.sessions[clusterID]
	if !ok || s.client == nil || s.state != domain.ClusterConnected {
		return nil, fmt.Errorf("cluster is not connected")
	}
	return s.client, nil
}

// Hub returns the watch hub for a connected cluster.
func (m *Manager) Hub(clusterID string) (*kube.WatchHub, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	s, ok := m.sessions[clusterID]
	if !ok || s.hub == nil {
		return nil, fmt.Errorf("cluster is not connected")
	}
	return s.hub, nil
}

// Namespaces returns the namespaces read at connect time.
func (m *Manager) Namespaces(clusterID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[clusterID]; ok {
		return append([]string(nil), s.namespaces...)
	}
	return nil
}

// APIResources returns what the cluster serves, so the sidebar can hide kinds
// this cluster does not have.
func (m *Manager) APIResources(clusterID string) []kube.APIResource {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if s, ok := m.sessions[clusterID]; ok {
		return append([]kube.APIResource(nil), s.resources...)
	}
	return nil
}

// Connect runs the full connection sequence and reports the resulting session.
//
// Each step is recorded as a probe even when it succeeds, so a later failure
// can be shown in context rather than as a bare error string.
func (m *Manager) Connect(ctx context.Context, clusterID string) (domain.Session, error) {
	cluster, err := m.repo.Get(clusterID)
	if err != nil {
		return domain.Session{}, err
	}

	m.mu.Lock()
	if _, busy := m.connecting[clusterID]; busy {
		m.mu.Unlock()
		return m.Session(clusterID), nil
	}
	m.connecting[clusterID] = struct{}{}
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.connecting, clusterID)
		m.mu.Unlock()
	}()

	m.transition(cluster, domain.ClusterConnecting, nil, "")

	probes := &builder{}

	if diag := m.checkAccess(ctx, cluster, probes); diag != nil {
		m.transition(cluster, domain.ClusterWaitingAccess, diag, diag.Summary)
		return m.Session(clusterID), nil
	}

	path, err := m.configs.PathFor(cluster.KubeconfigRef)
	if err != nil {
		diag := probes.diagnosis(domain.FailureConfig, "This cluster's kubeconfig is missing.", err.Error(), "")
		m.transition(cluster, domain.ClusterFailed, diag, diag.Summary)
		return m.Session(clusterID), nil
	}

	if elapsed, err := probeTCP(ctx, cluster.HostPort()); err != nil {
		kind, summary := classify(err)
		probes.fail(domain.LayerTCP, fmt.Sprintf("%s did not accept a connection", cluster.HostPort()), elapsed)
		probes.skipRest(domain.LayerTLS, domain.LayerKubernetes)

		detail := err.Error()
		profileID := ""
		if cluster.Access.Required {
			profileID = cluster.Access.ProfileID
			detail = "Customer network access is connected, but the API server port did not answer. " + detail
		}
		diag := probes.diagnosis(kind, summary, detail, profileID)
		m.transition(cluster, stateFor(kind), diag, summary)
		return m.Session(clusterID), nil
	} else {
		probes.pass(domain.LayerTCP, cluster.HostPort()+" accepted a connection", elapsed)
	}

	client, err := m.factory.Build(path, cluster.ContextName)
	if err != nil {
		probes.fail(domain.LayerKubernetes, "the kubeconfig context could not be loaded", 0)
		diag := probes.diagnosis(domain.FailureConfig, "This kubeconfig context could not be loaded.", err.Error(), "")
		m.transition(cluster, domain.ClusterFailed, diag, diag.Summary)
		return m.Session(clusterID), nil
	}

	versionCtx, cancel := context.WithTimeout(ctx, kube.ConnectTimeout)
	defer cancel()

	started := time.Now()
	serverVersion, err := client.FetchServerVersion(versionCtx)
	if err != nil {
		kind, summary := classify(err)
		if kind == domain.FailureTLS {
			probes.fail(domain.LayerTLS, summary, time.Since(started))
			probes.skipRest(domain.LayerKubernetes)
		} else {
			probes.pass(domain.LayerTLS, "TLS handshake completed", time.Since(started))
			probes.fail(domain.LayerKubernetes, summary, time.Since(started))
		}
		diag := probes.diagnosis(kind, summary, err.Error(), "")
		m.transition(cluster, stateFor(kind), diag, summary)
		client.Close()
		return m.Session(clusterID), nil
	}
	probes.pass(domain.LayerTLS, "TLS handshake completed", 0)
	probes.pass(domain.LayerKubernetes, "API server answered as "+serverVersion, time.Since(started))

	client.ServerVersion = serverVersion

	// Namespace and discovery reads are best-effort. A service account scoped
	// to one namespace cannot list namespaces, and refusing to open the
	// cluster for that reason would make Biebie Kube useless exactly where
	// least privilege is practised.
	namespaces, _ := client.Namespaces(ctx)
	resources, _ := client.ServerResources(ctx)

	namespace := m.repo.Namespace(clusterID)
	if namespace == "" {
		namespace = defaultNamespace(namespaces)
	}

	hub := kube.NewWatchHub(client.StreamDynamic, func(gvr schema.GroupVersionResource, ns string) {
		m.emit(EventResourcesChanged, ResourceChange{
			ClusterID: clusterID,
			Group:     gvr.Group,
			Resource:  gvr.Resource,
			Namespace: ns,
		})
	})

	now := time.Now()

	m.mu.Lock()
	if previous, ok := m.sessions[clusterID]; ok && previous.hub != nil {
		previous.hub.Close()
	}
	m.sessions[clusterID] = &session{
		cluster:       cluster,
		state:         domain.ClusterConnected,
		namespace:     namespace,
		client:        client,
		hub:           hub,
		serverVersion: serverVersion,
		connectedAt:   &now,
		namespaces:    namespaces,
		resources:     resources,
	}
	view := m.sessionView(clusterID)
	m.mu.Unlock()

	m.emit(EventSessionChanged, view)
	return view, nil
}

// checkAccess verifies customer connectivity before Kubernetes is attempted.
//
// Returning a diagnosis means "do not continue": the cluster is unreachable
// for a reason Biebie Kube cannot fix, and the UI should offer Biebie Access
// rather than a Kubernetes error.
func (m *Manager) checkAccess(ctx context.Context, cluster domain.Cluster, probes *builder) *domain.Diagnosis {
	if !cluster.Access.Required {
		return nil
	}

	if m.access == nil || !m.access.Installed(ctx) {
		probes.fail(domain.LayerAccess, "Biebie Access is not running", 0)
		probes.skipRest(domain.LayerTCP, domain.LayerTLS, domain.LayerKubernetes)
		return probes.diagnosis(
			domain.FailureAccessDown,
			"Biebie Access is required to reach this cluster.",
			"This cluster is only reachable over a customer network. Start Biebie Access and connect the profile, then try again.",
			cluster.Access.ProfileID,
		)
	}

	status, err := m.access.Status(ctx, cluster.Access.ProfileID)
	if err != nil {
		probes.fail(domain.LayerAccess, err.Error(), 0)
		probes.skipRest(domain.LayerTCP, domain.LayerTLS, domain.LayerKubernetes)
		return probes.diagnosis(
			domain.FailureAccessDown,
			"Customer network access could not be confirmed.",
			err.Error(),
			cluster.Access.ProfileID,
		)
	}

	if !status.Connected {
		probes.fail(domain.LayerAccess, "profile "+cluster.Access.ProfileID+" is "+string(status.State), 0)
		probes.skipRest(domain.LayerTCP, domain.LayerTLS, domain.LayerKubernetes)
		return probes.diagnosis(
			domain.FailureAccessDown,
			"Customer network access is required.",
			strings.TrimSpace(status.Detail+" Connect this customer in Biebie Access, and Biebie Kube will retry on its own."),
			cluster.Access.ProfileID,
		)
	}

	detail := "connected"
	if status.AssignedIP != "" {
		detail = "connected as " + status.AssignedIP
	}
	probes.pass(domain.LayerAccess, detail, 0)
	return nil
}

// Disconnect closes a session and everything hanging off it.
func (m *Manager) Disconnect(clusterID string) domain.Session {
	m.mu.Lock()
	if s, ok := m.sessions[clusterID]; ok {
		if s.hub != nil {
			s.hub.Close()
		}
		if s.client != nil {
			s.client.Close()
		}
		delete(m.sessions, clusterID)
	}
	m.mu.Unlock()

	view := domain.Session{ClusterID: clusterID, State: domain.ClusterDisconnected}
	m.emit(EventSessionChanged, view)
	return view
}

// CloseAll ends every session, called when the application shuts down.
func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.hub != nil {
			s.hub.Close()
		}
		if s.client != nil {
			s.client.Close()
		}
		delete(m.sessions, id)
	}
}

// SetNamespace changes the namespace a session is showing and remembers it.
func (m *Manager) SetNamespace(clusterID, namespace string) error {
	m.mu.Lock()
	s, ok := m.sessions[clusterID]
	if ok {
		previous := s.namespace
		s.namespace = namespace
		if s.hub != nil && previous != namespace && previous != domain.AllNamespaces {
			s.hub.StopNamespace(previous)
		}
	}
	m.mu.Unlock()

	if !ok {
		return fmt.Errorf("cluster is not connected")
	}
	if err := m.repo.RememberNamespace(clusterID, namespace); err != nil {
		return err
	}
	m.emit(EventSessionChanged, m.Session(clusterID))
	return nil
}

// RetryWaiting reconnects clusters that were only waiting on a customer
// network, after Biebie Access reports that profile came up.
//
// This is what turns "connect the VPN" into a finished flow without the
// engineer restarting either application.
func (m *Manager) RetryWaiting(ctx context.Context, profileID string) {
	m.mu.RLock()
	var waiting []string
	for id, s := range m.sessions {
		if s.state == domain.ClusterWaitingAccess && s.cluster.Access.ProfileID == profileID {
			waiting = append(waiting, id)
		}
	}
	m.mu.RUnlock()

	for _, id := range waiting {
		if _, err := m.Connect(ctx, id); err != nil {
			continue
		}
	}
}

func (m *Manager) transition(cluster domain.Cluster, state domain.ClusterState, diag *domain.Diagnosis, message string) {
	m.mu.Lock()
	s, ok := m.sessions[cluster.ID]
	if !ok {
		s = &session{cluster: cluster, namespace: m.repo.Namespace(cluster.ID)}
		m.sessions[cluster.ID] = s
	}
	s.cluster = cluster
	s.state = state
	s.diagnosis = diag
	s.lastError = message
	if state != domain.ClusterConnected {
		s.connectedAt = nil
	}
	view := m.sessionView(cluster.ID)
	m.mu.Unlock()

	m.emit(EventSessionChanged, view)
}

func (m *Manager) emit(event string, data any) {
	if m.emitter != nil {
		m.emitter.Emit(event, data)
	}
}

// defaultNamespace prefers "default" when it exists, because that is where a
// kubeconfig without an explicit namespace points.
func defaultNamespace(namespaces []string) string {
	for _, ns := range namespaces {
		if ns == "default" {
			return ns
		}
	}
	if len(namespaces) > 0 {
		return namespaces[0]
	}
	return "default"
}
