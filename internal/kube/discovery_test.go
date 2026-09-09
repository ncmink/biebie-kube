package kube

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMergeResourceListsPrefersGroupVersion(t *testing.T) {
	lists := []*metav1.APIResourceList{
		{
			GroupVersion: "widgets.example.io/v1beta1",
			APIResources: []metav1.APIResource{{
				Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list"},
			}},
		},
		{
			GroupVersion: "widgets.example.io/v1",
			APIResources: []metav1.APIResource{{
				Name: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list", "watch"},
			}},
		},
	}
	preferred := map[string]string{"widgets.example.io": "v1"}
	merged := mergeResourceLists(lists, preferred)
	if len(merged) != 1 {
		t.Fatalf("merged = %+v", merged)
	}
	if merged[0].Version != "v1" {
		t.Fatalf("version = %q", merged[0].Version)
	}
	if len(merged[0].Verbs) != 3 {
		t.Fatalf("verbs = %+v", merged[0].Verbs)
	}
}

func TestMergeResourceListsSkipsSubresourcesAndNonListable(t *testing.T) {
	lists := []*metav1.APIResourceList{{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{
			{Name: "pods/log", Kind: "Pod", Namespaced: true, Verbs: []string{"get"}},
			{Name: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch"}},
			{Name: "bindings", Kind: "Binding", Namespaced: true, Verbs: []string{"create"}},
		},
	}}
	merged := mergeResourceLists(lists, nil)
	if len(merged) != 1 || merged[0].Resource != "pods" {
		t.Fatalf("merged = %+v", merged)
	}
}
