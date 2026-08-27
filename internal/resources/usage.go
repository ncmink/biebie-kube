package resources

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"biebie-kube/internal/domain"
)

// metricsTTL is how long pod usage is reused.
//
// metrics-server recomputes every fifteen seconds or so of its own accord, so
// asking it more often than that returns the same numbers at the cost of a
// request per table repaint.
const metricsTTL = 15 * time.Second

// usageRow is one pod's usage, already formatted for a table cell.
type usageRow struct {
	cpu    string
	memory string
}

// usageState is a cluster's pod usage and when it was last read.
type usageState struct {
	rows    map[string]usageRow
	fetched time.Time

	// busy keeps several tables opening at once from each starting their own
	// read of the same numbers.
	busy bool
}

// usageFor returns pod usage, reading it again when the cached copy has aged
// out.
//
// A table never waits on metrics-server unless it has nothing at all: a
// cluster where it is slow, overloaded or absent must still render its pods,
// with the two columns it cannot fill left empty rather than the page left
// blank.
func (s *Service) usageFor(ctx context.Context, clusterID string, wait bool) map[string]usageRow {
	s.mu.Lock()
	state, ok := s.usage[clusterID]
	if !ok {
		state = &usageState{}
		s.usage[clusterID] = state
	}
	held := state.rows
	stale := time.Since(state.fetched) >= metricsTTL
	read := stale && !state.busy
	if read {
		state.busy = true
	}
	s.mu.Unlock()

	if !read {
		return held
	}
	if wait && held == nil {
		fresh := s.readUsage(ctx, clusterID)
		s.storeUsage(clusterID, fresh)
		return fresh
	}

	go func() {
		fresh := s.readUsage(context.Background(), clusterID)
		s.storeUsage(clusterID, fresh)
		s.applyUsage(clusterID, fresh)
	}()
	return held
}

// readUsage asks metrics-server for every pod's usage.
//
// Cluster-wide rather than per namespace, because one request answers for
// every view of the cluster and metrics-server charges the same for both.
func (s *Service) readUsage(ctx context.Context, clusterID string) map[string]usageRow {
	client, err := s.clusters.Client(clusterID)
	if err != nil || client.Metrics == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(ctx, metricsTimeout)
	defer cancel()

	list, err := client.Metrics.MetricsV1beta1().PodMetricses(metav1.NamespaceAll).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil
	}

	out := make(map[string]usageRow, len(list.Items))
	for _, item := range list.Items {
		cpu := resource.Quantity{}
		memory := resource.Quantity{}
		// A pod's usage is the sum of its containers': the number an engineer
		// compares with the pod's requests, and with what the node has left.
		for _, container := range item.Containers {
			cpu.Add(*container.Usage.Cpu())
			memory.Add(*container.Usage.Memory())
		}
		out[RowKey(item.Namespace, item.Name)] = usageRow{
			cpu:    formatCPU(cpu.MilliValue()),
			memory: formatMemory(memory.Value()),
		}
	}
	return out
}

// metricsTimeout bounds the extra request a pod table makes.
const metricsTimeout = 5 * time.Second

func (s *Service) storeUsage(clusterID string, rows map[string]usageRow) {
	s.mu.Lock()
	defer s.mu.Unlock()

	state, ok := s.usage[clusterID]
	if !ok {
		state = &usageState{}
		s.usage[clusterID] = state
	}
	state.busy = false
	state.fetched = time.Now()
	// A failed read keeps the previous numbers rather than blanking the
	// columns: usage a few seconds old is more use than no usage at all.
	if rows != nil {
		state.rows = rows
	}
}

// applyUsage pushes freshly read usage into the pod tables that are open, and
// tells the frontend about the cells that changed.
func (s *Service) applyUsage(clusterID string, rows map[string]usageRow) {
	if rows == nil {
		return
	}

	s.mu.Lock()
	open := make(map[view]*table)
	for key, rendered := range s.tables {
		if key.clusterID == clusterID && key.kind == domain.KindPod {
			open[key] = rendered
		}
	}
	s.mu.Unlock()

	for key, rendered := range open {
		touched, reordered := rendered.setUsage(rows)
		delta, worth := rendered.patch(touched, reordered)
		if !worth {
			continue
		}
		s.emit(EventRows, RowsChanged{
			ClusterID: clusterID,
			Kind:      key.kind,
			Namespace: key.namespace,
			Upserts:   delta.Upserts,
			Removed:   delta.Removed,
			Order:     delta.Order,
			Total:     delta.Total,
			Matched:   delta.Matched,
			Loading:   delta.Loading,
			Token:     delta.Token,
		})
	}
}

// formatCPU renders millicores the way an engineer reads them: as millicores
// below a core, and as cores above it.
func formatCPU(milli int64) string {
	if milli <= 0 {
		return ""
	}
	if milli < 1000 {
		return fmt.Sprintf("%dm", milli)
	}
	return trimZeros(fmt.Sprintf("%.2f", float64(milli)/1000))
}

// formatMemory renders bytes in the binary units Kubernetes itself uses, so
// the value beside a limit of "512Mi" is comparable to it.
func formatMemory(bytes int64) string {
	switch {
	case bytes <= 0:
		return ""
	case bytes < 1<<20:
		return fmt.Sprintf("%dKi", bytes>>10)
	case bytes < 1<<30:
		return fmt.Sprintf("%dMi", bytes>>20)
	default:
		return trimZeros(fmt.Sprintf("%.1f", float64(bytes)/(1<<30))) + "Gi"
	}
}

func trimZeros(value string) string {
	if !strings.Contains(value, ".") {
		return value
	}
	return strings.TrimSuffix(strings.TrimRight(value, "0"), ".")
}
