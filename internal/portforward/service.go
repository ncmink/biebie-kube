// Package portforward manages local ports that reach into a cluster.
//
// Forwards are always bound to loopback. A forward exists so this machine can
// reach a customer's service; publishing it on the LAN would quietly expose a
// customer's internal service to the network the laptop is sitting on.
package portforward

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/transport/spdy"

	"github.com/google/uuid"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
)

// loopback is the only address a forward is ever bound to.
const loopback = "127.0.0.1"

// EventChanged tells the frontend the forward list moved.
const EventChanged = "portforward:changed"

// readyTimeout bounds how long a forward may take to come up before it is
// reported as failed.
const readyTimeout = 10 * time.Second

// Emitter publishes changes to the frontend.
type Emitter interface {
	Emit(event string, data any)
}

// Service owns running forwards.
type Service struct {
	clusters *cluster.Manager
	emitter  Emitter

	mu       sync.Mutex
	sessions map[string]*forward
}

type forward struct {
	info   domain.PortForwardSession
	stop   chan struct{}
	closed sync.Once
}

// NewService wires the service.
func NewService(clusters *cluster.Manager, emitter Emitter) *Service {
	return &Service{
		clusters: clusters,
		emitter:  emitter,
		sessions: make(map[string]*forward),
	}
}

// Start opens a forward and waits for it to become usable.
//
// Waiting matters: returning while the tunnel is still connecting would show a
// "Running" row whose URL refuses connections, and the user would blame the
// service rather than the timing.
func (s *Service) Start(ctx context.Context, clusterID string, req domain.PortForwardRequest) (domain.PortForwardSession, error) {
	if err := req.Validate(); err != nil {
		return domain.PortForwardSession{}, err
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.PortForwardSession{}, err
	}

	podName := req.ResourceName
	remotePort := req.RemotePort
	if req.ResourceType == "service" {
		podName, remotePort, err = s.resolveService(ctx, clusterID, req)
		if err != nil {
			return domain.PortForwardSession{}, err
		}
	}

	localPort := req.LocalPort
	if localPort == 0 {
		if localPort, err = freePort(); err != nil {
			return domain.PortForwardSession{}, err
		}
	} else if !portAvailable(localPort) {
		return domain.PortForwardSession{}, fmt.Errorf("local port %d is already in use", localPort)
	}

	transport, upgrader, err := spdy.RoundTripperFor(client.RESTConfig)
	if err != nil {
		return domain.PortForwardSession{}, fmt.Errorf("prepare port forward: %w", err)
	}

	url := client.Clientset.CoreV1().RESTClient().
		Post().
		Resource("pods").
		Namespace(req.Namespace).
		Name(podName).
		SubResource("portforward").
		URL()

	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)

	stop := make(chan struct{})
	ready := make(chan struct{})
	errs := make(chan error, 1)

	forwarder, err := portforward.NewOnAddresses(
		dialer,
		[]string{loopback},
		[]string{fmt.Sprintf("%d:%d", localPort, remotePort)},
		stop,
		ready,
		nil,
		nil,
	)
	if err != nil {
		return domain.PortForwardSession{}, fmt.Errorf("prepare port forward: %w", err)
	}

	// The cluster name is denormalised into the session so the port-forward
	// panel can say which customer a localhost port belongs to without
	// resolving clusters on every render.
	clusterName := ""
	if record, err := s.clusters.Cluster(clusterID); err == nil {
		clusterName = record.Title()
	}

	session := &forward{
		info: domain.PortForwardSession{
			ID:           "pf_" + uuid.NewString(),
			ClusterID:    clusterID,
			ClusterName:  clusterName,
			Namespace:    req.Namespace,
			ResourceType: req.ResourceType,
			ResourceName: req.ResourceName,
			LocalPort:    localPort,
			RemotePort:   remotePort,
			State:        domain.PortForwardStarting,
			StartedAt:    time.Now(),
		},
		stop: stop,
	}

	s.mu.Lock()
	s.sessions[session.info.ID] = session
	s.mu.Unlock()

	go func() {
		if err := forwarder.ForwardPorts(); err != nil {
			errs <- err
		}
		s.finish(session.info.ID, err)
	}()

	select {
	case <-ready:
		s.setState(session.info.ID, domain.PortForwardRunning, "")
	case err := <-errs:
		s.setState(session.info.ID, domain.PortForwardFailed, err.Error())
		return s.get(session.info.ID), fmt.Errorf("start port forward: %w", err)
	case <-time.After(readyTimeout):
		s.Stop(session.info.ID)
		return domain.PortForwardSession{}, errors.New("the port forward did not become ready in time")
	}

	return s.get(session.info.ID), nil
}

// resolveService picks a pod behind a service, and maps the service port to
// the container port that actually listens.
func (s *Service) resolveService(ctx context.Context, clusterID string, req domain.PortForwardRequest) (string, int, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return "", 0, err
	}

	service, err := client.Clientset.CoreV1().Services(req.Namespace).Get(ctx, req.ResourceName, metav1.GetOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("read service %s: %w", req.ResourceName, err)
	}
	if len(service.Spec.Selector) == 0 {
		return "", 0, fmt.Errorf("service %s has no selector, so it has no pods to forward to", req.ResourceName)
	}

	targetPort := req.RemotePort
	for _, port := range service.Spec.Ports {
		if int(port.Port) != req.RemotePort {
			continue
		}
		if port.TargetPort.IntValue() != 0 {
			targetPort = port.TargetPort.IntValue()
		}
	}

	selector := labels.SelectorFromSet(service.Spec.Selector).String()
	pods, err := client.Clientset.CoreV1().Pods(req.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return "", 0, fmt.Errorf("find pods for %s: %w", req.ResourceName, err)
	}
	for _, pod := range pods.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.DeletionTimestamp == nil {
			return pod.Name, targetPort, nil
		}
	}
	return "", 0, fmt.Errorf("service %s has no running pods", req.ResourceName)
}

// Stop closes one forward.
func (s *Service) Stop(id string) {
	s.mu.Lock()
	session, ok := s.sessions[id]
	s.mu.Unlock()
	if !ok {
		return
	}
	session.closed.Do(func() { close(session.stop) })

	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()

	s.publish()
}

// StopCluster closes every forward belonging to a cluster, which happens when
// that cluster disconnects: the tunnel underneath is gone anyway.
func (s *Service) StopCluster(clusterID string) {
	s.mu.Lock()
	var ids []string
	for id, session := range s.sessions {
		if session.info.ClusterID == clusterID {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()

	for _, id := range ids {
		s.Stop(id)
	}
}

// StopAll closes every forward, on shutdown.
func (s *Service) StopAll() {
	s.mu.Lock()
	ids := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		ids = append(ids, id)
	}
	s.mu.Unlock()

	for _, id := range ids {
		s.Stop(id)
	}
}

// Sessions lists forwards, grouped so the panel reads cluster by cluster.
func (s *Service) Sessions() []domain.PortForwardSession {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]domain.PortForwardSession, 0, len(s.sessions))
	for _, session := range s.sessions {
		out = append(out, session.info)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].ClusterID != out[j].ClusterID {
			return out[i].ClusterID < out[j].ClusterID
		}
		return out[i].StartedAt.Before(out[j].StartedAt)
	})
	return out
}

// Find returns a running forward that already reaches one object.
//
// A second request for the same service reuses the tunnel instead of opening
// another. Two forwards to one Argo CD server would be two rows in the panel
// and two ports for the same page, and closing the one the engineer can see
// would leave the other holding a socket.
func (s *Service) Find(clusterID, namespace, resourceType, resourceName string) (domain.PortForwardSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, session := range s.sessions {
		info := session.info
		if info.State != domain.PortForwardRunning {
			continue
		}
		if info.ClusterID == clusterID &&
			info.Namespace == namespace &&
			info.ResourceType == resourceType &&
			info.ResourceName == resourceName {
			return info, true
		}
	}
	return domain.PortForwardSession{}, false
}

func (s *Service) get(id string) domain.PortForwardSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[id]; ok {
		return session.info
	}
	return domain.PortForwardSession{ID: id, State: domain.PortForwardStopped}
}

func (s *Service) setState(id string, state domain.PortForwardState, message string) {
	s.mu.Lock()
	if session, ok := s.sessions[id]; ok {
		session.info.State = state
		session.info.Error = message
	}
	s.mu.Unlock()
	s.publish()
}

// finish records that a forward's goroutine ended.
//
// A forward dies when its pod is replaced, which is common during a rollout,
// so the row is marked stopped with a reason instead of vanishing.
func (s *Service) finish(id string, err error) {
	s.mu.Lock()
	session, ok := s.sessions[id]
	if ok {
		session.info.State = domain.PortForwardStopped
		if err != nil {
			session.info.State = domain.PortForwardFailed
			session.info.Error = strings.TrimSpace(err.Error())
		}
	}
	s.mu.Unlock()
	s.publish()
}

func (s *Service) publish() {
	if s.emitter != nil {
		s.emitter.Emit(EventChanged, s.Sessions())
	}
}

// freePort asks the operating system for an unused loopback port.
func freePort() (int, error) {
	listener, err := net.Listen("tcp", loopback+":0")
	if err != nil {
		return 0, fmt.Errorf("find a free local port: %w", err)
	}
	defer func() { _ = listener.Close() }()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func portAvailable(port int) bool {
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", loopback, port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}
