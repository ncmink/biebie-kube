package gitops

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// gather reads the cluster facts replica analysis needs, once per comparison.
//
// HPAs in this namespace and the owning Application. Nothing here talks to
// Git. Failures are recorded on the result rather than returned, so a
// forbidden list cannot take away a difference that was already computed.
func (s *Service) gather(ctx context.Context, clusterID string, ownership domain.ResourceOwnership, live *subject) analysis {
	out := analysis{live: live.object}
	out.hpas, out.hpaErr = s.listHPAs(ctx, clusterID, live.object.GetNamespace())
	if ownership.App != nil {
		out.app, out.argoErr = s.readApplication(ctx, clusterID, ownership.App.Namespace, ownership.App.Name)
	}
	return out
}

func (s *Service) listHPAs(ctx context.Context, clusterID, namespace string) ([]*unstructured.Unstructured, error) {
	if namespace == "" {
		return nil, nil
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, err
	}

	// v2 first, because that is what the catalogue names. A cluster that only
	// still serves v1 is asked second rather than treated as having no HPAs.
	for _, gvr := range []kube.APIResource{
		{Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers"},
		{Group: "autoscaling", Version: "v1", Resource: "horizontalpodautoscalers"},
	} {
		list, err := client.Dynamic.Resource(kube.GVRFor(gvr.Group, gvr.Version, gvr.Resource)).
			Namespace(namespace).List(ctx, metav1.ListOptions{})
		if err == nil {
			out := make([]*unstructured.Unstructured, 0, len(list.Items))
			for i := range list.Items {
				out = append(out, &list.Items[i])
			}
			return out, nil
		}
		if apierrors.IsNotFound(err) {
			continue
		}
		return nil, err
	}
	return nil, nil
}

func (s *Service) readApplication(ctx context.Context, clusterID, namespace, name string) (*unstructured.Unstructured, error) {
	if namespace == "" || name == "" {
		return nil, nil
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, err
	}
	return client.Dynamic.Resource(kube.GVRFor("argoproj.io", "v1alpha1", "applications")).
		Namespace(namespace).Get(ctx, name, metav1.GetOptions{})
}
