package cluster

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	bctx "github.com/ncmink/biebie-protocol/context"

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

// ResourceChange tells the frontend which resource type went stale.
//
// It carries no rows. The rendering layer turns the same notification into a
// patch for the table in view; this event is what the parts of the UI that
// only count objects — the sidebar's empty-state fading — listen to.
type ResourceChange struct {
	ClusterID string `json:"clusterId"`
	Resource  string `json:"resource"`
	Group     string `json:"group"`
	Namespace string `json:"namespace"`
}

// ResourceSink receives a cluster's watch notifications.
//
// The manager knows when a watch fired. It does not know what a table looks
// like, so the rendering layer registers here rather than the manager reaching
// into it.
type ResourceSink func(clusterID string, change kube.Change)

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

	// catalogue is the navigation this cluster serves, and kinds is the same
	// list keyed for lookup. Both are per-session because custom resources are
	// a property of the cluster: the kind a customer's operator installed does
	// not exist in the cluster next to it.
	catalogue []domain.KindInfo
	kinds     map[domain.Kind]domain.KindInfo

	// apiForward and forwards are what Biebie Access was lending when this
	// session was established: the local port standing in for the API server,
	// if any, and everything else the same connection opened.
	apiForward *bctx.Forward
	forwards   []bctx.Forward

	// gateway is the machine those forwards terminate on. Both ends of a
	// forward are usually written as loopback, so without a name for the far
	// side the page cannot say which address belongs to which machine.
	gateway string

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

	sink ResourceSink
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

// OnResources registers the sink for watch notifications, once at startup.
func (m *Manager) OnResources(sink ResourceSink) {
	m.mu.Lock()
	m.sink = sink
	m.mu.Unlock()
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
		APIForward:    s.apiForward,
		Forwards:      s.forwards,
		Gateway:       s.gateway,
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

// Catalogue returns the navigation tree for a cluster: the built-in kinds it
// actually serves, plus the custom ones its own definitions declare.
//
// A cluster that is not connected has told us nothing, so the compiled-in
// catalogue stands. It is what the sidebar shows before a connection, and it
// keeps every built-in kind addressable without a session.
func (m *Manager) Catalogue(clusterID string) []domain.KindInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if s, ok := m.sessions[clusterID]; ok && len(s.catalogue) > 0 {
		return append([]domain.KindInfo(nil), s.catalogue...)
	}
	return domain.Catalogue()
}

// LookupKind resolves a kind for one cluster.
//
// Custom kinds are found only through a session, because only the cluster
// knows them. Built-in kinds resolve either way, so the compiled-in catalogue
// is the fallback rather than an error.
func (m *Manager) LookupKind(clusterID string, kind domain.Kind) (domain.KindInfo, bool) {
	m.mu.RLock()
	if s, ok := m.sessions[clusterID]; ok {
		if info, found := s.kinds[kind]; found {
			m.mu.RUnlock()
			return info, true
		}
	}
	m.mu.RUnlock()

	return domain.Lookup(kind)
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

	network := m.checkAccess(ctx, cluster, probes)

	path, err := m.configs.PathFor(cluster.KubeconfigRef)
	if err != nil {
		diag := probes.diagnosis(domain.FailureConfig, "This cluster's kubeconfig is missing.", err.Error(), "")
		m.transition(cluster, domain.ClusterFailed, diag, diag.Summary)
		return m.Session(clusterID), nil
	}

	// Where the API server actually answers. It is the cluster's own address
	// unless Biebie Access is standing a local port in for it.
	endpoint, via := network.endpointFor(cluster)

	if elapsed, err := probeTCP(ctx, endpoint); err != nil {
		kind, summary := classify(err)
		probes.fail(domain.LayerTCP, fmt.Sprintf("%s did not accept a connection", network.describe(cluster)), elapsed)
		probes.skipRest(domain.LayerTLS, domain.LayerKubernetes)

		detail := err.Error()
		profileID := ""
		switch {
		case network.viaTunnel:
			profileID = cluster.Access.ProfileID
			detail = "Biebie Access is forwarding " + endpoint + " to " + cluster.HostPort() +
				", but nothing answered there. " + detail
		case network.confirmed:
			profileID = cluster.Access.ProfileID
			detail = "Customer network access is connected, but the API server port did not answer. " + detail
		case network.required:
			// The port stayed silent and the customer network was never
			// confirmed up, so that is the likeliest thing to fix — and the
			// waiting state lets the retry fire the moment it comes up.
			profileID = cluster.Access.ProfileID
			kind, summary = domain.FailureAccessDown, "Customer network access is required."
			detail = network.reason + " " + detail
		}
		diag := probes.diagnosis(kind, summary, detail, profileID)
		m.transition(cluster, stateFor(kind), diag, summary)
		return m.Session(clusterID), nil
	} else {
		probes.pass(domain.LayerTCP, network.describe(cluster)+" accepted a connection", elapsed)
	}

	client, err := m.factory.BuildVia(path, cluster.ContextName, via)
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
		switch kind {
		case domain.FailureTLS:
			probes.fail(domain.LayerTLS, summary, time.Since(started))
			probes.skipRest(domain.LayerKubernetes)
		case domain.FailureCredentialHelper:
			// client-go runs the exec plugin before it opens a connection, so
			// no request left the machine. Reporting a completed handshake
			// here would be inventing a result the code never observed, and it
			// is exactly the reassuring line that makes a PATH problem look
			// like a cluster problem.
			probes.skip(domain.LayerTLS, "no request was sent, so TLS was not reached")
			probes.fail(domain.LayerKubernetes, summary, time.Since(started))
		default:
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

	// Definitions are read once per connection rather than per navigation: the
	// set only changes when someone installs an operator, and the sidebar needs
	// it before the first click. An account that may not list them cluster-wide
	// gets a navigation without a custom section, which is the truth for it.
	customs, _ := client.CustomResources(ctx)
	catalogue := catalogueFor(resources, customs)

	kinds := make(map[domain.Kind]domain.KindInfo, len(catalogue))
	for _, info := range catalogue {
		kinds[info.Kind] = info
	}

	namespace := m.repo.Namespace(clusterID)
	if namespace == "" {
		namespace = defaultNamespace(namespaces)
	}

	hub := kube.NewWatchHub(client.StreamDynamic, func(change kube.Change) {
		m.notifyResources(clusterID, change)
	})

	now := time.Now()

	m.mu.Lock()
	if previous, ok := m.sessions[clusterID]; ok && previous.hub != nil {
		previous.hub.Close()
	}
	live := &session{
		cluster:       cluster,
		state:         domain.ClusterConnected,
		namespace:     namespace,
		client:        client,
		hub:           hub,
		serverVersion: serverVersion,
		connectedAt:   &now,
		namespaces:    namespaces,
		resources:     resources,
		catalogue:     catalogue,
		kinds:         kinds,
		forwards:      network.extras,
		gateway:       network.gateway,
	}
	if network.viaTunnel {
		forward := network.forward
		live.apiForward = &forward
	}
	m.sessions[clusterID] = live
	view := m.sessionView(clusterID)
	m.mu.Unlock()

	m.emit(EventSessionChanged, view)
	return view, nil
}

// accessCheck is what Biebie Access could say about a cluster's customer
// network before the connection was attempted.
type accessCheck struct {
	required bool
	// confirmed is true only when Biebie Access answered and reported the
	// customer network up.
	confirmed bool
	// reason explains, in the engineer's terms, why it was not confirmed.
	reason string

	// viaTunnel reports that the API server is not reachable at its own
	// address and forward is the local port standing in for it.
	viaTunnel bool
	forward   bctx.Forward

	// extras are the forwards this connection lends that are not the API
	// server — a NodePort service, a database — which the cluster page offers
	// as links.
	extras []bctx.Forward

	// gateway is the machine the forwards terminate on, named so the page can
	// tell the two loopback addresses in a forward apart.
	gateway string
}

// endpointFor resolves where to dial and, when that is not the cluster's own
// address, how the client should verify what answers there.
func (a accessCheck) endpointFor(cluster domain.Cluster) (string, *kube.Endpoint) {
	if !a.viaTunnel {
		return cluster.HostPort(), nil
	}
	local := a.forward.Local()
	return local, &kube.Endpoint{Address: local, ServerName: cluster.Host()}
}

// describe names the endpoint for the diagnosis panel.
//
// A bare loopback address in a cluster's probe list reads as a bug — the
// engineer knows perfectly well their cluster is not on this machine — so the
// tunnel is spelled out wherever the substituted address appears.
func (a accessCheck) describe(cluster domain.Cluster) string {
	if !a.viaTunnel {
		return cluster.HostPort()
	}
	return a.forward.Local() + ", forwarded by Biebie Access to " + cluster.HostPort()
}

// checkAccess asks Biebie Access about the customer network this cluster sits
// behind. It advises; it does not decide.
//
// Biebie Access is one way to reach a customer network, not the only one: the
// engineer may already be on site, on the customer's own VPN client, or the
// endpoint may be public despite the cluster being marked as needing access.
// Refusing to dial because Biebie Access is absent would call those clusters
// unreachable without ever having looked. So an unconfirmed network is recorded
// as untested and the connection is tried anyway — if the network does answer,
// nothing was ever wrong, and if it does not, the caller has the reason to hand.
func (m *Manager) checkAccess(ctx context.Context, cluster domain.Cluster, probes *builder) accessCheck {
	if !cluster.Access.Required {
		return accessCheck{}
	}

	unconfirmed := func(probe, reason string) accessCheck {
		probes.skip(domain.LayerAccess, probe)
		return accessCheck{required: true, reason: reason}
	}

	if m.access == nil || !m.access.Installed(ctx) {
		return unconfirmed(
			"Biebie Access is not running; the network is tried directly",
			"Biebie Access is not running on this machine, so the customer network could not be checked or brought up for you.",
		)
	}

	status, err := m.access.Status(ctx, cluster.Access.ProfileID)
	if err != nil {
		return unconfirmed(err.Error(), "Customer network access could not be confirmed: "+err.Error()+".")
	}

	if !status.Connected {
		return unconfirmed(
			"profile "+cluster.Access.ProfileID+" is "+string(status.State),
			strings.TrimSpace(status.Detail+" Connect this customer in Biebie Access, and Biebie Kube will retry on its own."),
		)
	}

	check := accessCheck{required: true, confirmed: true}

	detail := "connected"
	if status.AssignedIP != "" {
		detail = "connected as " + status.AssignedIP
	}

	// A connection that lends local ports has told us which addresses inside
	// the customer network they stand for. If one of them is this cluster's API
	// server, that local port is the only way to reach it — the cluster's own
	// address is behind the firewall the tunnel exists to get around.
	if forward, ok := status.LocalFor(cluster.Host(), cluster.Port()); ok {
		check.viaTunnel = true
		check.forward = forward
		detail = "connected, forwarding " + forward.Local() + " to " + cluster.HostPort()
	}
	check.extras = otherForwards(status.Forwards, check.forward, check.viaTunnel)
	check.gateway = status.Gateway

	probes.pass(domain.LayerAccess, detail, 0)
	return check
}

// otherForwards is everything the connection lends that is not this cluster's
// API server. They belong to the customer network rather than to Kubernetes —
// a NodePort service, an admin console — and the cluster page is simply the
// place the engineer is already looking when they need one.
func otherForwards(all []bctx.Forward, api bctx.Forward, hasAPI bool) []bctx.Forward {
	var out []bctx.Forward
	for _, f := range all {
		if hasAPI && f.LocalPort == api.LocalPort {
			continue
		}
		out = append(out, f)
	}
	return out
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
		if s.state != domain.ClusterWaitingAccess {
			continue
		}
		// The cluster is re-read rather than taken from the session. Its Biebie
		// Access reference may have been a connection name that has since been
		// rewritten to the identifier this notification arrives under, and the
		// session began waiting before that happened.
		cluster, err := m.repo.Get(id)
		if err != nil {
			cluster = s.cluster
		}
		if cluster.Access.ProfileID == profileID {
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

// notifyResources hands a watch notification to the rendering layer and tells
// the UI which resource type moved.
//
// Both happen: the sink patches the table in view, and the coarse event is
// what the sidebar's counts key off, which no patch would tell them.
func (m *Manager) notifyResources(clusterID string, change kube.Change) {
	m.mu.RLock()
	sink := m.sink
	m.mu.RUnlock()

	if sink != nil {
		sink(clusterID, change)
	}
	m.emit(EventResourcesChanged, ResourceChange{
		ClusterID: clusterID,
		Group:     change.GVR.Group,
		Resource:  change.GVR.Resource,
		Namespace: change.Namespace,
	})
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
