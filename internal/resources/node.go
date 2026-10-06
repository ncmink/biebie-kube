package resources

import (
	"context"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"

	"biebie-kube/internal/domain"
)

// nodeListLimit bounds one node's pod and event lists.
//
// A node that holds more than this is already past the scheduler's pod
// ceiling, and Explain Why only needs a sample of what is stuck.
const nodeListLimit = 500

// kubeSystem is where a cluster's node agents are installed.
const kubeSystem = "kube-system"

// NodeDetail reads one node as typed conditions and allocatable pod slots.
func (s *Service) NodeDetail(ctx context.Context, clusterID, name string) (domain.NodeDetail, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.NodeDetail{}, err
	}

	node, err := client.Clientset.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.NodeDetail{}, fmt.Errorf("read node %s: %w", name, err)
	}

	detail := domain.NodeDetail{
		Name:     node.Name,
		Ready:    nodeReady(*node),
		Cordoned: node.Spec.Unschedulable,
		MaxPods:  int(node.Status.Allocatable.Pods().Value()),
	}
	for _, condition := range node.Status.Conditions {
		item := domain.Condition{
			Type:    string(condition.Type),
			Status:  string(condition.Status),
			Reason:  condition.Reason,
			Message: condition.Message,
		}
		if !condition.LastTransitionTime.IsZero() {
			since := condition.LastTransitionTime.Time
			item.Since = &since
		}
		detail.Conditions = append(detail.Conditions, item)
	}
	return detail, nil
}

// NodePods lists the pods assigned to one node.
//
// maxPods is the node's allocatable pod ceiling, already read with the node,
// so this list does not GET the node again. The field selector keeps the read
// to one node instead of every pod in the cluster.
func (s *Service) NodePods(ctx context.Context, clusterID, nodeName string, maxPods int) (domain.NodePods, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.NodePods{}, err
	}

	list, err := client.Clientset.CoreV1().Pods(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: fields.OneTermEqualSelector("spec.nodeName", nodeName).String(),
		Limit:         nodeListLimit,
	})
	if err != nil {
		return domain.NodePods{}, fmt.Errorf("list pods on node %s: %w", nodeName, err)
	}

	out := domain.NodePods{
		MaxPods: maxPods,
		ByPhase: map[string]int{},
	}
	for _, pod := range list.Items {
		out.ByPhase[string(pod.Status.Phase)]++
		if countsTowardNode(pod) {
			out.PodsUsed++
		}
		if stuck, reason := stuckPod(pod); stuck {
			out.Stuck = append(out.Stuck, domain.NodeStuckPod{
				Namespace: pod.Namespace,
				Name:      pod.Name,
				Phase:     string(pod.Status.Phase),
				Reason:    reason,
			})
		}
		if agent, ok := networkAgent(pod); ok {
			out.Agents = append(out.Agents, agent)
		}
	}
	sort.Slice(out.Stuck, func(i, j int) bool {
		if out.Stuck[i].Namespace != out.Stuck[j].Namespace {
			return out.Stuck[i].Namespace < out.Stuck[j].Namespace
		}
		return out.Stuck[i].Name < out.Stuck[j].Name
	})
	sort.Slice(out.Agents, func(i, j int) bool { return out.Agents[i].Name < out.Agents[j].Name })
	return out, nil
}

// NodeEvents lists events about one node.
//
// Node events are cluster-scoped in practice and usually land in "default",
// so the read is across namespaces. Filtering on kind as well as name keeps a
// pod that happens to share the node's name out of the report.
func (s *Service) NodeEvents(ctx context.Context, clusterID, nodeName string) ([]domain.EventRow, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, err
	}

	selector := fields.AndSelectors(
		fields.OneTermEqualSelector("involvedObject.kind", "Node"),
		fields.OneTermEqualSelector("involvedObject.name", nodeName),
	)
	list, err := client.Clientset.CoreV1().Events(metav1.NamespaceAll).List(ctx, metav1.ListOptions{
		FieldSelector: selector.String(),
		Limit:         nodeListLimit,
	})
	if err != nil {
		return nil, fmt.Errorf("list events for node %s: %w", nodeName, err)
	}

	out := make([]domain.EventRow, 0, len(list.Items))
	for _, event := range list.Items {
		out = append(out, eventRow(event))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}

// stuckPod reports a pod that has been given a node but still has no
// containers: ContainerCreating, or Pending after the scheduler accepted it.
func stuckPod(pod corev1.Pod) (bool, string) {
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return false, ""
	}
	if reason := waitingReason(pod); reason == "ContainerCreating" {
		return true, reason
	}
	if pod.Status.Reason == "ContainerCreating" {
		return true, pod.Status.Reason
	}
	if pod.Status.Phase == corev1.PodPending && podScheduled(pod) {
		reason := pod.Status.Reason
		if reason == "" {
			reason = "Pending"
		}
		return true, reason
	}
	return false, ""
}

func waitingReason(pod corev1.Pod) string {
	for _, status := range append(pod.Status.InitContainerStatuses, pod.Status.ContainerStatuses...) {
		if status.State.Waiting != nil && status.State.Waiting.Reason != "" {
			return status.State.Waiting.Reason
		}
	}
	return ""
}

func podScheduled(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodScheduled {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// networkAgent reports a kube-system DaemonSet pod on this node that is not
// ready. The pod's name is returned as the cluster wrote it; the caller does
// not interpret it.
func networkAgent(pod corev1.Pod) (domain.NodeAgentPod, bool) {
	if pod.Namespace != kubeSystem || podReady(pod) || !ownedByDaemonSet(pod) {
		return domain.NodeAgentPod{}, false
	}
	agent := domain.NodeAgentPod{
		Namespace: pod.Namespace,
		Name:      pod.Name,
		Phase:     string(pod.Status.Phase),
	}
	byName := map[string]corev1.ContainerStatus{}
	for _, status := range pod.Status.InitContainerStatuses {
		byName[status.Name] = status
	}
	for _, container := range pod.Spec.InitContainers {
		agent.InitContainers = append(agent.InitContainers, agentContainer(container.Name, true, byName[container.Name]))
	}
	byName = map[string]corev1.ContainerStatus{}
	for _, status := range pod.Status.ContainerStatuses {
		byName[status.Name] = status
	}
	for _, container := range pod.Spec.Containers {
		agent.Containers = append(agent.Containers, agentContainer(container.Name, false, byName[container.Name]))
	}
	return agent, true
}

func ownedByDaemonSet(pod corev1.Pod) bool {
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

func podReady(pod corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func agentContainer(name string, init bool, status corev1.ContainerStatus) domain.NodeAgentContainer {
	out := domain.NodeAgentContainer{Name: name, Init: init}
	switch {
	case status.State.Waiting != nil:
		out.State = status.State.Waiting.Reason
	case status.State.Terminated != nil:
		out.State = status.State.Terminated.Reason
	case status.State.Running != nil:
		out.State = "Running"
	}
	// The previous termination is the one that says why an init container
	// died. The current waiting reason is only "CrashLoopBackOff".
	switch {
	case status.LastTerminationState.Terminated != nil:
		out.LastTerminationReason = status.LastTerminationState.Terminated.Reason
		out.LastTerminationMessage = status.LastTerminationState.Terminated.Message
	case status.State.Terminated != nil:
		out.LastTerminationReason = status.State.Terminated.Reason
		out.LastTerminationMessage = status.State.Terminated.Message
	case status.State.Waiting != nil:
		out.LastTerminationMessage = status.State.Waiting.Message
	}
	return out
}
