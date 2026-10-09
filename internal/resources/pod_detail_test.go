package resources

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestProbeInfoHTTPGet(t *testing.T) {
	got := probeInfo(&corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/api/health",
				Port: intstr.FromInt(3000),
			},
		},
		InitialDelaySeconds: 60,
		TimeoutSeconds:      30,
		PeriodSeconds:       10,
		SuccessThreshold:    1,
		FailureThreshold:    10,
	})
	if got == nil || got.Kind != "http-get" || got.Target != "http://:3000/api/health" {
		t.Fatalf("probe = %+v", got)
	}
	if got.InitialDelay != 60 || got.Timeout != 30 || got.Period != 10 || got.SuccessThreshold != 1 || got.FailureThreshold != 10 {
		t.Fatalf("thresholds = %+v", got)
	}
}

func TestTolerationLine(t *testing.T) {
	seconds := int64(300)
	got := tolerationLine(corev1.Toleration{
		Key:               "node.kubernetes.io/not-ready",
		Operator:          corev1.TolerationOpExists,
		Effect:            corev1.TaintEffectNoExecute,
		TolerationSeconds: &seconds,
	})
	want := "node.kubernetes.io/not-ready:NoExecute op=Exists for 300s"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestControlledByUsesController(t *testing.T) {
	yes := true
	got := controlledBy([]metav1.OwnerReference{
		{Kind: "ReplicaSet", Name: "grafana-6f968f6b87", Controller: &yes},
	})
	if got != "ReplicaSet grafana-6f968f6b87" {
		t.Fatalf("got %q", got)
	}
}

func TestVolumeType(t *testing.T) {
	if got := volumeType(corev1.Volume{VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}); got != "Empty Dir" {
		t.Fatalf("got %q", got)
	}
}
