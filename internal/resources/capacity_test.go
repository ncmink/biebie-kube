package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNodeCapacityIgnoresCompletedPods(t *testing.T) {
	nodes := []corev1.Node{testNode("node-a", "4", "8Gi", "110")}
	pods := []corev1.Pod{
		testPod("running", "node-a", corev1.PodRunning, "100m", "128Mi"),
		testPod("done", "node-a", corev1.PodSucceeded, "500m", "1Gi"),
		testPod("failed", "node-a", corev1.PodFailed, "500m", "1Gi"),
	}

	capacity, unscheduled := nodeCapacity(nodes, pods, nil)
	if unscheduled != 0 {
		t.Fatalf("unscheduled = %d", unscheduled)
	}
	if len(capacity) != 1 {
		t.Fatalf("nodes = %d", len(capacity))
	}
	if capacity[0].PodsUsed != 1 {
		t.Fatalf("pods used = %d; completed pods must not count", capacity[0].PodsUsed)
	}
	if capacity[0].CPURequestMilli != 100 {
		t.Fatalf("cpu request = %d", capacity[0].CPURequestMilli)
	}
}

func TestNodeCapacityInitContainerMax(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "job"},
		Spec: corev1.PodSpec{
			NodeName: "node-a",
			InitContainers: []corev1.Container{{
				Name: "init",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("2"),
						corev1.ResourceMemory: resource.MustParse("4Gi"),
					},
				},
			}},
			Containers: []corev1.Container{{
				Name: "main",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	capacity, _ := nodeCapacity([]corev1.Node{testNode("node-a", "8", "16Gi", "110")}, []corev1.Pod{pod}, nil)
	if capacity[0].CPURequestMilli != 2000 {
		t.Fatalf("cpu request = %d; init max should win over container sum", capacity[0].CPURequestMilli)
	}
	if capacity[0].MemoryRequestBytes != 4*1024*1024*1024 {
		t.Fatalf("memory request = %d", capacity[0].MemoryRequestBytes)
	}
}

func TestNodeCapacityOverhead(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "sandbox"},
		Spec: corev1.PodSpec{
			NodeName: "node-a",
			Overhead: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("50m"),
				corev1.ResourceMemory: resource.MustParse("64Mi"),
			},
			Containers: []corev1.Container{{
				Name: "pause",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("10m"),
						corev1.ResourceMemory: resource.MustParse("32Mi"),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}

	capacity, _ := nodeCapacity([]corev1.Node{testNode("node-a", "4", "8Gi", "110")}, []corev1.Pod{pod}, nil)
	if capacity[0].CPURequestMilli != 60 {
		t.Fatalf("cpu request = %d; overhead must be included", capacity[0].CPURequestMilli)
	}
}

func TestNodeCapacityUnscheduledPending(t *testing.T) {
	pods := []corev1.Pod{{
		ObjectMeta: metav1.ObjectMeta{Name: "waiting"},
		Spec:       corev1.PodSpec{},
		Status:     corev1.PodStatus{Phase: corev1.PodPending},
	}}

	_, unscheduled := nodeCapacity([]corev1.Node{testNode("node-a", "4", "8Gi", "110")}, pods, nil)
	if unscheduled != 1 {
		t.Fatalf("unscheduled = %d", unscheduled)
	}
}

func TestNodeCapacityUsageAndEmptyNode(t *testing.T) {
	usage := map[string]corev1.ResourceList{
		"node-a": {
			corev1.ResourceCPU:    resource.MustParse("1500m"),
			corev1.ResourceMemory: resource.MustParse("2Gi"),
		},
	}
	capacity, _ := nodeCapacity(
		[]corev1.Node{
			testNode("node-a", "4", "8Gi", "110"),
			testNode("node-b", "4", "8Gi", "110"),
		},
		nil,
		usage,
	)
	if capacity[0].CPUUsedMilli != 1500 {
		t.Fatalf("cpu used = %d", capacity[0].CPUUsedMilli)
	}
	if capacity[1].PodsUsed != 0 || capacity[1].CPUUsedMilli != 0 {
		t.Fatalf("empty node should have zero usage and pods: %+v", capacity[1])
	}
}

func testNode(name, cpu, memory, pods string) corev1.Node {
	return corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Allocatable: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpu),
				corev1.ResourceMemory: resource.MustParse(memory),
				corev1.ResourcePods:   resource.MustParse(pods),
			},
			Conditions: []corev1.NodeCondition{{
				Type:   corev1.NodeReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
}

func testPod(name, node string, phase corev1.PodPhase, cpu, memory string) corev1.Pod {
	return corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: corev1.PodSpec{
			NodeName: node,
			Containers: []corev1.Container{{
				Name: "app",
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(cpu),
						corev1.ResourceMemory: resource.MustParse(memory),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse(cpu),
						corev1.ResourceMemory: resource.MustParse(memory),
					},
				},
			}},
		},
		Status: corev1.PodStatus{Phase: phase},
	}
}
