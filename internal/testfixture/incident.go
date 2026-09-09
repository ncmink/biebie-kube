package testfixture

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"

	"biebie-kube/internal/kube"
)

// DiscoveryBaseline documents reusable discovery inputs for regression tests.
//
// These fixtures intentionally avoid live clusters. They model restricted RBAC,
// partial API groups and CRD enrichment gaps described in incident-workspace-spec.
var DiscoveryBaseline = struct {
	CoreAndApps []kube.APIResource
	WidgetOnly  []kube.APIResource
	PartialApps kube.DiscoveryResult
	WidgetCRD   kube.CustomResource
	ForbiddenCRDError error
}{
	CoreAndApps: []kube.APIResource{
		{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch"}},
		{Group: "apps", Version: "v1", Resource: "deployments", Kind: "Deployment", Namespaced: true, Verbs: []string{"get", "list", "watch"}},
	},
	WidgetOnly: []kube.APIResource{
		{Group: "widgets.example.io", Version: "v1", Resource: "widgets", Kind: "Widget", Namespaced: true, Verbs: []string{"get", "list"}},
	},
	PartialApps: kube.DiscoveryResult{
		Resources: []kube.APIResource{
			{Group: "", Version: "v1", Resource: "pods", Kind: "Pod", Namespaced: true, Verbs: []string{"get", "list", "watch"}},
		},
		Issues: []kube.DiscoveryIssue{{
			Group:   "apps",
			Code:    "forbidden",
			Message: "apps.apiserver is forbidden",
		}},
		Complete: false,
	},
	WidgetCRD: kube.CustomResource{
		Group: "widgets.example.io", Version: "v1", Plural: "widgets", Kind: "Widget",
		Namespaced: true,
		Columns: []kube.PrinterColumn{{Name: "Phase", JSONPath: ".status.phase"}},
	},
	ForbiddenCRDError: errForbidden("customresourcedefinitions.apiextensions.k8s.io is forbidden"),
}

type forbiddenError string

func (e forbiddenError) Error() string { return string(e) }

func errForbidden(msg string) error { return forbiddenError(msg) }

// PodCrashLoop is a pod fixture for future Explain Why rules (IW-03).
func PodCrashLoop(name, namespace string) *corev1.Pod {
	now := metav1.Now()
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID("pod-crash-" + name)},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name: "app",
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off restarting failed container"},
				},
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 1, FinishedAt: now},
				},
				RestartCount: 5,
			}},
		},
	}
}

// PodOOMKilled keeps termination evidence after the container returns to running.
func PodOOMKilled(name, namespace string) *corev1.Pod {
	finished := metav1.NewTime(time.Now().Add(-2 * time.Minute))
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, UID: types.UID("pod-oom-" + name)},
		Status: corev1.PodStatus{
			Phase: corev1.PodRunning,
			ContainerStatuses: []corev1.ContainerStatus{{
				Name:  "app",
				Ready: true,
				State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{StartedAt: finished}},
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled", ExitCode: 137, FinishedAt: finished},
				},
				RestartCount: 2,
			}},
		},
	}
}

// WidgetCRDObject is a minimal CRD definition used in fake-client tests.
func WidgetCRDObject() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apiextensions.k8s.io/v1",
		"kind":       "CustomResourceDefinition",
		"metadata":   map[string]any{"name": "widgets.widgets.example.io"},
		"spec": map[string]any{
			"group": "widgets.example.io",
			"scope": "Namespaced",
			"names": map[string]any{"plural": "widgets", "kind": "Widget", "listKind": "WidgetList"},
			"versions": []any{
				map[string]any{
					"name": "v1", "served": true, "storage": true,
					"additionalPrinterColumns": []any{
						map[string]any{"name": "Phase", "type": "string", "jsonPath": ".status.phase"},
					},
				},
			},
		},
	}}
}
