package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNetworkAgentKeepsInitTerminationMessage(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent-abc",
			Namespace: "kube-system",
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "DaemonSet",
				Name: "agent",
			}},
		},
		Spec: corev1.PodSpec{
			InitContainers: []corev1.Container{{Name: "install"}},
			Containers:     []corev1.Container{{Name: "agent"}},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			InitContainerStatuses: []corev1.ContainerStatus{{
				Name: "install",
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff", Message: "back-off restarting failed container"},
				},
				LastTerminationState: corev1.ContainerState{
					Terminated: &corev1.ContainerStateTerminated{
						Reason:  "Error",
						Message: "iptables: Memory allocation problem.",
					},
				},
			}},
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionFalse,
			}},
		},
	}

	agent, ok := networkAgent(pod)
	if !ok {
		t.Fatal("not-ready kube-system DaemonSet pod was ignored")
	}
	if len(agent.InitContainers) != 1 {
		t.Fatalf("init containers = %d", len(agent.InitContainers))
	}
	if agent.InitContainers[0].LastTerminationMessage != "iptables: Memory allocation problem." {
		t.Fatalf("message = %q; the backoff text hid the termination", agent.InitContainers[0].LastTerminationMessage)
	}
}

func TestReadyKubeSystemDaemonSetIsNotAnAgent(t *testing.T) {
	pod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "agent-abc",
			Namespace: "kube-system",
			OwnerReferences: []metav1.OwnerReference{{
				Kind: "DaemonSet",
				Name: "agent",
			}},
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			}},
		},
	}
	if _, ok := networkAgent(pod); ok {
		t.Fatal("a ready DaemonSet pod is not evidence of a failing network agent")
	}
}
