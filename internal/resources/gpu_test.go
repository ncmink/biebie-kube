package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIsGPUResourceName(t *testing.T) {
	if !IsGPUResourceName("nvidia.com/gpu") {
		t.Fatal("nvidia.com/gpu should be a GPU resource")
	}
	if !IsGPUResourceName("nvidia.com/mig-1g.5gb") {
		t.Fatal("MIG resources should match nvidia.com/")
	}
	if IsGPUResourceName("cpu") || IsGPUResourceName("hugepages-2Mi") {
		t.Fatal("standard resources must not match")
	}
}

func TestPodGPURequestsInitMax(t *testing.T) {
	pod := corev1.Pod{
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{
				Name: "init",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
					},
				},
			}},
			Containers: []corev1.Container{{
				Name: "main",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
				},
			}},
		},
	}
	req := podGPURequests(pod)
	if req["nvidia.com/gpu"] != 2 {
		t.Fatalf("requests = %v; init max should win", req)
	}
}

func TestNodeGPUCapacityMergesAllocAndRequested(t *testing.T) {
	out := nodeGPUCapacity(
		map[string]int64{"nvidia.com/gpu": 4},
		map[string]int64{"nvidia.com/gpu": 3},
	)
	if len(out) != 1 || out[0].Allocatable != 4 || out[0].Requested != 3 {
		t.Fatalf("capacity = %+v", out)
	}
}

func TestNodeCapacityGPUAggregation(t *testing.T) {
	node := testNode("gpu-node", "4", "8Gi", "110")
	node.Status.Allocatable[corev1.ResourceName("nvidia.com/gpu")] = resource.MustParse("4")

	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "trainer"},
		Spec: corev1.PodSpec{
			NodeName: "gpu-node",
			Containers: []corev1.Container{{
				Name: "main",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("2"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	capacity, _, unscheduledGPU := nodeCapacity([]corev1.Node{node}, []corev1.Pod{pod}, nil)
	if unscheduledGPU != 0 {
		t.Fatalf("unscheduled gpu = %d", unscheduledGPU)
	}
	if len(capacity[0].GPU) != 1 || capacity[0].GPU[0].Requested != 2 {
		t.Fatalf("gpu = %+v", capacity[0].GPU)
	}
}

func TestNodeCapacityUnscheduledGPU(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "waiting"},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{
				Name: "main",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceName("nvidia.com/gpu"): resource.MustParse("1"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodPending},
	}

	_, unscheduled, unscheduledGPU := nodeCapacity([]corev1.Node{testNode("n", "4", "8Gi", "110")}, []corev1.Pod{pod}, nil)
	if unscheduled != 1 || unscheduledGPU != 1 {
		t.Fatalf("unscheduled=%d gpu=%d", unscheduled, unscheduledGPU)
	}
}
