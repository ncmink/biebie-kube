package resources

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"biebie-kube/internal/domain"
)

// Overview builds the cluster dashboard.
//
// Every read is best-effort. An account scoped to one namespace cannot count
// nodes, and a cluster without metrics-server cannot report usage; both are
// ordinary situations that must render a useful page rather than an error.
func (s *Service) Overview(ctx context.Context, clusterID string) (domain.ClusterOverview, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.ClusterOverview{}, err
	}

	overview := domain.ClusterOverview{
		ClusterID:     clusterID,
		ServerVersion: client.ServerVersion,
	}

	if nodes, err := client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{}); err == nil {
		overview.Nodes.Total = len(nodes.Items)
		for _, node := range nodes.Items {
			if nodeReady(node) {
				overview.Nodes.Ready++
			}
			if overview.Platform == "" {
				overview.Platform = node.Status.NodeInfo.OSImage
			}
		}
		overview.Metrics = s.nodeMetrics(ctx, clusterID, nodes.Items)
	}

	if pods, err := client.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{}); err == nil {
		overview.Pods.Total = len(pods.Items)
		for _, pod := range pods.Items {
			if pod.Status.Phase == corev1.PodRunning || pod.Status.Phase == corev1.PodSucceeded {
				overview.Pods.Ready++
			}
		}
	}

	if namespaces, err := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{}); err == nil {
		overview.Namespaces = len(namespaces.Items)
	}
	if deployments, err := client.Clientset.AppsV1().Deployments("").List(ctx, metav1.ListOptions{}); err == nil {
		overview.Deployments = len(deployments.Items)
	}
	if statefulSets, err := client.Clientset.AppsV1().StatefulSets("").List(ctx, metav1.ListOptions{}); err == nil {
		overview.StatefulSets = len(statefulSets.Items)
	}
	if daemonSets, err := client.Clientset.AppsV1().DaemonSets("").List(ctx, metav1.ListOptions{}); err == nil {
		overview.DaemonSets = len(daemonSets.Items)
	}

	if events, err := s.Events(ctx, clusterID, "", ""); err == nil {
		for _, event := range events {
			if event.Type != "Warning" {
				continue
			}
			overview.RecentWarnings = append(overview.RecentWarnings, event)
			if len(overview.RecentWarnings) == 10 {
				break
			}
		}
	}

	return overview, nil
}

// nodeMetrics aggregates usage, returning nil when metrics-server is absent.
func (s *Service) nodeMetrics(ctx context.Context, clusterID string, nodes []corev1.Node) *domain.ClusterMetrics {
	client, err := s.clusters.Client(clusterID)
	if err != nil || client.Metrics == nil {
		return nil
	}

	usage, err := client.Metrics.MetricsV1beta1().NodeMetricses().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	metrics := &domain.ClusterMetrics{}
	for _, node := range nodes {
		metrics.CPUCapacityMilli += node.Status.Capacity.Cpu().MilliValue()
		metrics.MemoryCapacityBytes += node.Status.Capacity.Memory().Value()
	}
	for _, item := range usage.Items {
		metrics.CPUUsedMilli += item.Usage.Cpu().MilliValue()
		metrics.MemoryUsedBytes += item.Usage.Memory().Value()
	}
	return metrics
}

func nodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}
