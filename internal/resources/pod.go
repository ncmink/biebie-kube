package resources

import (
	"context"
	"fmt"
	"sort"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"biebie-kube/internal/domain"
)

// PodDetail reads the overview tab of a pod.
func (s *Service) PodDetail(ctx context.Context, clusterID, namespace, name string) (domain.PodDetail, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.PodDetail{}, err
	}

	pod, err := client.Clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return domain.PodDetail{}, fmt.Errorf("read pod %s: %w", name, err)
	}

	detail := domain.PodDetail{
		Ref:         domain.ResourceRef{Kind: domain.KindPod, Namespace: namespace, Name: name},
		Status:      string(pod.Status.Phase),
		Node:        pod.Spec.NodeName,
		PodIP:       pod.Status.PodIP,
		HostIP:      pod.Status.HostIP,
		QOSClass:    string(pod.Status.QOSClass),
		Labels:      pod.Labels,
		Annotations: pod.Annotations,
	}
	if pod.Status.StartTime != nil {
		started := pod.Status.StartTime.Time
		detail.StartedAt = &started
	}

	statuses := make(map[string]int32, len(pod.Status.ContainerStatuses))
	ready := make(map[string]bool, len(pod.Status.ContainerStatuses))
	states := make(map[string]string, len(pod.Status.ContainerStatuses))
	for _, status := range append(pod.Status.ContainerStatuses, pod.Status.InitContainerStatuses...) {
		statuses[status.Name] = status.RestartCount
		ready[status.Name] = status.Ready
		switch {
		case status.State.Waiting != nil:
			states[status.Name] = status.State.Waiting.Reason
		case status.State.Terminated != nil:
			states[status.Name] = status.State.Terminated.Reason
		case status.State.Running != nil:
			states[status.Name] = "Running"
		}
	}

	for _, container := range pod.Spec.Containers {
		detail.Containers = append(detail.Containers, domain.ContainerInfo{
			Name:         container.Name,
			Image:        container.Image,
			Ready:        ready[container.Name],
			State:        states[container.Name],
			RestartCount: statuses[container.Name],
		})
		for _, port := range container.Ports {
			detail.Ports = append(detail.Ports, domain.ContainerPort{
				Name:     port.Name,
				Port:     port.ContainerPort,
				Protocol: string(port.Protocol),
			})
		}
	}
	for _, container := range pod.Spec.InitContainers {
		detail.InitContainers = append(detail.InitContainers, domain.ContainerInfo{
			Name:         container.Name,
			Image:        container.Image,
			Ready:        ready[container.Name],
			State:        states[container.Name],
			RestartCount: statuses[container.Name],
			Init:         true,
		})
	}

	for _, volume := range pod.Spec.Volumes {
		detail.Volumes = append(detail.Volumes, volume.Name)
	}
	for _, condition := range pod.Status.Conditions {
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

	detail.Health = podHealth(detail)
	return detail, nil
}

func podHealth(detail domain.PodDetail) domain.Health {
	if detail.Status == "Succeeded" {
		return domain.HealthHealthy
	}
	for _, container := range detail.Containers {
		switch container.State {
		case "CrashLoopBackOff", "ImagePullBackOff", "ErrImagePull", "CreateContainerConfigError":
			return domain.HealthCritical
		}
		if !container.Ready {
			return domain.HealthWarning
		}
	}
	if detail.Status == "Running" {
		return domain.HealthHealthy
	}
	return domain.HealthProgress
}

// Containers lists a pod's containers for the log and terminal selectors,
// without reading the rest of the pod.
func (s *Service) Containers(ctx context.Context, clusterID, namespace, pod string) ([]domain.ContainerInfo, error) {
	detail, err := s.PodDetail(ctx, clusterID, namespace, pod)
	if err != nil {
		return nil, err
	}
	return append(detail.InitContainers, detail.Containers...), nil
}

// Events lists events, newest first.
//
// Events for one object are read with a field selector so the detail tab does
// not pull a whole namespace of unrelated activity.
func (s *Service) Events(ctx context.Context, clusterID, namespace string, involving string) ([]domain.EventRow, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, err
	}

	options := metav1.ListOptions{Limit: 500}
	if involving != "" {
		options.FieldSelector = "involvedObject.name=" + involving
	}

	list, err := client.Clientset.CoreV1().Events(namespace).List(ctx, options)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}

	out := make([]domain.EventRow, 0, len(list.Items))
	for _, event := range list.Items {
		row := domain.EventRow{
			UID:       string(event.UID),
			Type:      event.Type,
			Reason:    event.Reason,
			Object:    event.InvolvedObject.Kind + "/" + event.InvolvedObject.Name,
			Message:   event.Message,
			Namespace: event.Namespace,
			Count:     event.Count,
			FirstSeen: event.FirstTimestamp.Time,
			LastSeen:  lastSeen(event.LastTimestamp.Time, event.EventTime.Time, event.FirstTimestamp.Time),
		}
		out = append(out, row)
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}

// lastSeen copes with the two event APIs.
//
// Core events fill lastTimestamp; events written through events.k8s.io fill
// eventTime and leave the old field zero, which would sort every modern event
// to the bottom of the list.
func lastSeen(last, eventTime, first time.Time) time.Time {
	if !last.IsZero() {
		return last
	}
	if !eventTime.IsZero() {
		return eventTime
	}
	return first
}
