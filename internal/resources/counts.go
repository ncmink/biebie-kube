package resources

import (
	"context"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// Counts reports whether each catalogue kind has objects, for the sidebar's
// empty-state fading. A kind the account cannot list is omitted rather than
// reported as zero, so a permission gap is not drawn as "none".
func (s *Service) Counts(ctx context.Context, clusterID, namespace string) ([]domain.KindPresence, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, err
	}

	var mu sync.Mutex
	out := make([]domain.KindPresence, 0, 32)
	var wg sync.WaitGroup

	for _, info := range domain.Catalogue() {
		info := info
		wg.Add(1)
		go func() {
			defer wg.Done()
			count, err := s.countKind(ctx, clusterID, client, info, namespace)
			if err != nil {
				return
			}
			mu.Lock()
			out = append(out, domain.KindPresence{Kind: info.Kind, Count: count})
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out, nil
}

func (s *Service) countKind(
	ctx context.Context,
	clusterID string,
	client *kube.ClusterClient,
	info domain.KindInfo,
	namespace string,
) (int, error) {
	if !info.Namespaced {
		namespace = domain.AllNamespaces
	}
	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)

	if hub, err := s.clusters.Hub(clusterID); err == nil {
		if watch := hub.Existing(gvr, namespace); watch != nil {
			cached, err := watch.List(namespace)
			if err == nil {
				return len(cached), nil
			}
		}
	}

	// Limit 1 is enough to tell empty from not: the sidebar only fades zeros.
	opts := metav1.ListOptions{Limit: 1}
	if namespace == domain.AllNamespaces {
		result, err := client.Dynamic.Resource(gvr).List(ctx, opts)
		if err != nil {
			return 0, err
		}
		return len(result.Items), nil
	}
	result, err := client.Dynamic.Resource(gvr).Namespace(namespace).List(ctx, opts)
	if err != nil {
		return 0, err
	}
	return len(result.Items), nil
}
