package resources

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"biebie-kube/internal/domain"
)

// PodDetail reads the scrolling detail page of a pod.
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
		Ref:            domain.ResourceRef{Kind: domain.KindPod, Namespace: namespace, Name: name},
		CreatedAt:      pod.CreationTimestamp.Time,
		Status:         string(pod.Status.Phase),
		Node:           pod.Spec.NodeName,
		PodIP:          pod.Status.PodIP,
		HostIP:         pod.Status.HostIP,
		QOSClass:       string(pod.Status.QOSClass),
		ServiceAccount: pod.Spec.ServiceAccountName,
		ControlledBy:   controlledBy(pod.OwnerReferences),
		Labels:         pod.Labels,
		Annotations:    pod.Annotations,
	}
	if pod.Status.StartTime != nil {
		started := pod.Status.StartTime.Time
		detail.StartedAt = &started
	}
	for _, address := range pod.Status.PodIPs {
		detail.PodIPs = append(detail.PodIPs, address.IP)
	}

	byName := make(map[string]*corev1.ContainerStatus, len(pod.Status.ContainerStatuses)+len(pod.Status.InitContainerStatuses))
	for i := range pod.Status.ContainerStatuses {
		status := &pod.Status.ContainerStatuses[i]
		byName[status.Name] = status
	}
	for i := range pod.Status.InitContainerStatuses {
		status := &pod.Status.InitContainerStatuses[i]
		byName[status.Name] = status
	}

	for _, container := range pod.Spec.Containers {
		info := containerInfo(container, byName[container.Name], false)
		detail.Containers = append(detail.Containers, info)
		detail.Ports = append(detail.Ports, info.Ports...)
	}
	for _, container := range pod.Spec.InitContainers {
		detail.InitContainers = append(detail.InitContainers, containerInfo(container, byName[container.Name], true))
	}

	for _, volume := range pod.Spec.Volumes {
		detail.Volumes = append(detail.Volumes, domain.PodVolume{
			Name: volume.Name,
			Type: volumeType(volume),
		})
	}
	for _, toleration := range pod.Spec.Tolerations {
		detail.Tolerations = append(detail.Tolerations, tolerationLine(toleration))
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

func containerInfo(container corev1.Container, status *corev1.ContainerStatus, init bool) domain.ContainerInfo {
	info := domain.ContainerInfo{
		Name:      container.Name,
		Image:     container.Image,
		Init:      init,
		Ports:     containerPorts(container.Ports),
		Env:       envVars(container.Env),
		EnvFrom:   envFromLines(container.EnvFrom),
		Mounts:    volumeMounts(container.VolumeMounts),
		Command:   container.Command,
		Args:      container.Args,
		Liveness:  probeInfo(container.LivenessProbe),
		Readiness: probeInfo(container.ReadinessProbe),
		Startup:   probeInfo(container.StartupProbe),
		Requests:  resourceMap(container.Resources.Requests),
		Limits:    resourceMap(container.Resources.Limits),
	}
	if status == nil {
		return info
	}

	info.Ready = status.Ready
	info.RestartCount = status.RestartCount
	switch {
	case status.State.Waiting != nil:
		info.State = status.State.Waiting.Reason
	case status.State.Terminated != nil:
		info.State = status.State.Terminated.Reason
		info.StartedAt = timePtr(status.State.Terminated.StartedAt.Time)
	case status.State.Running != nil:
		info.State = "Running"
		info.StartedAt = timePtr(status.State.Running.StartedAt.Time)
	}
	if terminated := status.LastTerminationState.Terminated; terminated != nil {
		info.LastTerminationReason = terminated.Reason
		info.LastExitCode = terminated.ExitCode
		info.LastStartedAt = timePtr(terminated.StartedAt.Time)
		info.LastFinishedAt = timePtr(terminated.FinishedAt.Time)
	}
	return info
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func containerPorts(ports []corev1.ContainerPort) []domain.ContainerPort {
	if len(ports) == 0 {
		return nil
	}
	out := make([]domain.ContainerPort, 0, len(ports))
	for _, port := range ports {
		protocol := string(port.Protocol)
		if protocol == "" {
			protocol = string(corev1.ProtocolTCP)
		}
		out = append(out, domain.ContainerPort{
			Name:     port.Name,
			Port:     port.ContainerPort,
			Protocol: protocol,
		})
	}
	return out
}

func envVars(env []corev1.EnvVar) []domain.EnvVar {
	if len(env) == 0 {
		return nil
	}
	out := make([]domain.EnvVar, 0, len(env))
	for _, item := range env {
		variable := domain.EnvVar{Name: item.Name, Value: item.Value}
		if item.ValueFrom != nil {
			variable.From = valueFrom(item.ValueFrom)
		}
		out = append(out, variable)
	}
	return out
}

func valueFrom(src *corev1.EnvVarSource) string {
	switch {
	case src.SecretKeyRef != nil:
		return "secret/" + src.SecretKeyRef.Name + ":" + src.SecretKeyRef.Key
	case src.ConfigMapKeyRef != nil:
		return "configmap/" + src.ConfigMapKeyRef.Name + ":" + src.ConfigMapKeyRef.Key
	case src.FieldRef != nil:
		return "field " + src.FieldRef.FieldPath
	case src.ResourceFieldRef != nil:
		name := src.ResourceFieldRef.Resource
		if src.ResourceFieldRef.ContainerName != "" {
			name = src.ResourceFieldRef.ContainerName + ":" + name
		}
		return "resource " + name
	default:
		return ""
	}
}

func envFromLines(sources []corev1.EnvFromSource) []string {
	if len(sources) == 0 {
		return nil
	}
	out := make([]string, 0, len(sources))
	for _, src := range sources {
		var line string
		switch {
		case src.ConfigMapRef != nil:
			line = "configmap/" + src.ConfigMapRef.Name
		case src.SecretRef != nil:
			line = "secret/" + src.SecretRef.Name
		default:
			continue
		}
		if src.Prefix != "" {
			line += " (prefix " + src.Prefix + ")"
		}
		out = append(out, line)
	}
	return out
}

func volumeMounts(mounts []corev1.VolumeMount) []domain.VolumeMount {
	if len(mounts) == 0 {
		return nil
	}
	out := make([]domain.VolumeMount, 0, len(mounts))
	for _, mount := range mounts {
		out = append(out, domain.VolumeMount{
			Name:     mount.Name,
			Path:     mount.MountPath,
			ReadOnly: mount.ReadOnly,
			SubPath:  mount.SubPath,
		})
	}
	return out
}

func probeInfo(probe *corev1.Probe) *domain.ContainerProbe {
	if probe == nil {
		return nil
	}
	out := &domain.ContainerProbe{
		InitialDelay:     probe.InitialDelaySeconds,
		Timeout:          probe.TimeoutSeconds,
		Period:           probe.PeriodSeconds,
		SuccessThreshold: probe.SuccessThreshold,
		FailureThreshold: probe.FailureThreshold,
	}
	switch {
	case probe.HTTPGet != nil:
		out.Kind = "http-get"
		scheme := string(probe.HTTPGet.Scheme)
		if scheme == "" {
			scheme = "HTTP"
		}
		out.Target = strings.ToLower(scheme) + "://" + probe.HTTPGet.Host + ":" + probe.HTTPGet.Port.String() + probe.HTTPGet.Path
	case probe.TCPSocket != nil:
		out.Kind = "tcp-socket"
		out.Target = probe.TCPSocket.Port.String()
		if probe.TCPSocket.Host != "" {
			out.Target = probe.TCPSocket.Host + ":" + out.Target
		}
	case probe.Exec != nil:
		out.Kind = "exec"
		out.Target = strings.Join(probe.Exec.Command, " ")
	case probe.GRPC != nil:
		out.Kind = "grpc"
		out.Target = fmt.Sprintf("%d", probe.GRPC.Port)
		if probe.GRPC.Service != nil && *probe.GRPC.Service != "" {
			out.Target = *probe.GRPC.Service + ":" + out.Target
		}
	default:
		out.Kind = "probe"
	}
	return out
}

func resourceMap(list corev1.ResourceList) map[string]string {
	if len(list) == 0 {
		return nil
	}
	out := make(map[string]string, len(list))
	for name, quantity := range list {
		out[string(name)] = quantity.String()
	}
	return out
}

func volumeType(volume corev1.Volume) string {
	switch {
	case volume.ConfigMap != nil:
		return "Config Map"
	case volume.PersistentVolumeClaim != nil:
		return "Persistent Volume Claim"
	case volume.EmptyDir != nil:
		return "Empty Dir"
	case volume.Projected != nil:
		return "Projected"
	case volume.Secret != nil:
		return "Secret"
	case volume.HostPath != nil:
		return "Host Path"
	case volume.DownwardAPI != nil:
		return "Downward API"
	case volume.CSI != nil:
		return "CSI"
	case volume.Ephemeral != nil:
		return "Ephemeral"
	default:
		return "Volume"
	}
}

func tolerationLine(item corev1.Toleration) string {
	key := item.Key
	if key == "" {
		key = "*"
	}
	effect := string(item.Effect)
	if effect == "" {
		effect = "all"
	}
	line := key
	if item.Value != "" {
		line += "=" + item.Value
	}
	line += ":" + effect
	op := string(item.Operator)
	if op == "" {
		op = string(corev1.TolerationOpEqual)
	}
	line += " op=" + op
	if item.TolerationSeconds != nil {
		line += fmt.Sprintf(" for %ds", *item.TolerationSeconds)
	}
	return line
}

func controlledBy(refs []metav1.OwnerReference) string {
	for _, ref := range refs {
		if ref.Controller != nil && *ref.Controller {
			return ref.Kind + " " + ref.Name
		}
	}
	return ""
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
		out = append(out, eventRow(event))
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out, nil
}

func eventRow(event corev1.Event) domain.EventRow {
	return domain.EventRow{
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
