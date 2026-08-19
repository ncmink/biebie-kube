package resources

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sjson "k8s.io/apimachinery/pkg/util/json"

	"biebie-kube/internal/domain"
)

// object decodes a fixture the way client-go decodes a live object.
//
// The standard library turns every JSON number into a float64, while
// apimachinery produces int64 — so decoding fixtures with encoding/json would
// test against a shape the cluster never sends.
func object(t *testing.T, raw string) *unstructured.Unstructured {
	t.Helper()
	var content map[string]any
	if err := k8sjson.Unmarshal([]byte(raw), &content); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return &unstructured.Unstructured{Object: content}
}

func TestRunningPodIsHealthy(t *testing.T) {
	pod := object(t, `{
		"metadata": {"name": "api-78d9f", "namespace": "default"},
		"spec": {"nodeName": "node-1", "containers": [{"name": "api"}]},
		"status": {"phase": "Running", "containerStatuses": [
			{"name": "api", "ready": true, "restartCount": 0, "state": {"running": {}}}
		]}
	}`)

	row := Row(domain.KindPod, pod)
	if row.Health != domain.HealthHealthy {
		t.Fatalf("health = %q", row.Health)
	}
	if row.Fields["ready"] != "1/1" || row.Fields["node"] != "node-1" {
		t.Fatalf("fields = %+v", row.Fields)
	}
}

func TestCrashLoopIsReportedInsteadOfRunning(t *testing.T) {
	pod := object(t, `{
		"metadata": {"name": "worker-79abc"},
		"spec": {"containers": [{"name": "worker"}]},
		"status": {"phase": "Running", "containerStatuses": [
			{"name": "worker", "ready": false, "restartCount": 7,
			 "state": {"waiting": {"reason": "CrashLoopBackOff"}}}
		]}
	}`)

	row := Row(domain.KindPod, pod)
	if row.Status != "CrashLoopBackOff" {
		t.Fatalf("status = %q; the waiting reason is what the engineer needs", row.Status)
	}
	if row.Health != domain.HealthCritical {
		t.Fatalf("health = %q", row.Health)
	}
	if row.Fields["restarts"] != "7" {
		t.Fatalf("restarts = %q", row.Fields["restarts"])
	}
}

func TestPartiallyReadyPodIsNotHealthy(t *testing.T) {
	pod := object(t, `{
		"metadata": {"name": "api"},
		"spec": {"containers": [{"name": "api"}, {"name": "sidecar"}]},
		"status": {"phase": "Running", "containerStatuses": [
			{"name": "api", "ready": true, "state": {"running": {}}},
			{"name": "sidecar", "ready": false, "state": {"running": {}}}
		]}
	}`)

	row := Row(domain.KindPod, pod)
	if row.Health == domain.HealthHealthy {
		t.Fatal("a pod with an unready container must not read as healthy")
	}
	if row.Fields["ready"] != "1/2" {
		t.Fatalf("ready = %q", row.Fields["ready"])
	}
}

func TestTerminatingResourceIsFlagged(t *testing.T) {
	pod := object(t, `{
		"metadata": {"name": "api", "deletionTimestamp": "2026-08-19T10:00:00Z"},
		"spec": {"containers": [{"name": "api"}]},
		"status": {"phase": "Running", "containerStatuses": [
			{"name": "api", "ready": true, "state": {"running": {}}}
		]}
	}`)

	row := Row(domain.KindPod, pod)
	if row.Status != "Terminating" {
		t.Fatalf("status = %q; a deleting pod still reports Running in its own status", row.Status)
	}
}

func TestDeploymentBelowDesiredIsProgressing(t *testing.T) {
	deployment := object(t, `{
		"metadata": {"name": "api"},
		"spec": {"replicas": 3},
		"status": {"readyReplicas": 1, "updatedReplicas": 3, "availableReplicas": 1}
	}`)

	row := Row(domain.KindDeployment, deployment)
	if row.Health != domain.HealthProgress {
		t.Fatalf("health = %q", row.Health)
	}
	if row.Fields["ready"] != "1/3" {
		t.Fatalf("ready = %q", row.Fields["ready"])
	}
}

func TestScaledToZeroReplicaSetIsNotCritical(t *testing.T) {
	rs := object(t, `{"metadata": {"name": "api-old"}, "spec": {"replicas": 0}, "status": {}}`)

	if row := Row(domain.KindReplicaSet, rs); row.Health != domain.HealthHealthy {
		t.Fatalf("health = %q; the remains of a rollout are not a problem", row.Health)
	}
}

func TestSecretRowCountsKeysWithoutReadingValues(t *testing.T) {
	secret := object(t, `{
		"metadata": {"name": "api-credentials"},
		"type": "Opaque",
		"data": {"password": "c2VjcmV0MTIz", "token": "ZXlKaGJHY2lPaQ=="}
	}`)

	row := Row(domain.KindSecret, secret)
	if row.Fields["keys"] != "2" {
		t.Fatalf("keys = %q", row.Fields["keys"])
	}
	for key, value := range row.Fields {
		if value == "c2VjcmV0MTIz" || value == "ZXlKaGJHY2lPaQ==" {
			t.Fatalf("field %q leaked a secret value into a list row", key)
		}
	}
}

func TestPendingLoadBalancerIsCalledOut(t *testing.T) {
	service := object(t, `{
		"metadata": {"name": "gateway"},
		"spec": {"type": "LoadBalancer", "clusterIP": "10.43.0.11", "ports": [{"port": 80, "protocol": "TCP"}]},
		"status": {"loadBalancer": {}}
	}`)

	row := Row(domain.KindService, service)
	if row.Status != "Pending" {
		t.Fatalf("status = %q; a LoadBalancer with no address is stuck", row.Status)
	}
	if row.Fields["ports"] != "80" {
		t.Fatalf("ports = %q", row.Fields["ports"])
	}
}

func TestCordonedNodeIsNotSilentlyReady(t *testing.T) {
	node := object(t, `{
		"metadata": {"name": "node-1", "labels": {"node-role.kubernetes.io/control-plane": ""}},
		"spec": {"unschedulable": true},
		"status": {
			"conditions": [{"type": "Ready", "status": "True"}],
			"nodeInfo": {"kubeletVersion": "v1.31.2+rke2r1"},
			"capacity": {"cpu": "8", "memory": "32900000Ki"}
		}
	}`)

	row := Row(domain.KindNode, node)
	if row.Status == "Ready" {
		t.Fatal("a cordoned node reports Ready, which hides why nothing schedules onto it")
	}
	if row.Fields["roles"] != "control-plane" {
		t.Fatalf("roles = %q", row.Fields["roles"])
	}
	if row.Fields["version"] != "v1.31.2+rke2r1" {
		t.Fatalf("version = %q", row.Fields["version"])
	}
}

func TestWarningEventIsHighlighted(t *testing.T) {
	event := object(t, `{
		"metadata": {"name": "api.17c"},
		"type": "Warning",
		"reason": "BackOff",
		"message": "Back-off restarting failed container",
		"count": 12,
		"involvedObject": {"kind": "Pod", "name": "api-78d9f"}
	}`)

	row := Row(domain.KindEvent, event)
	if row.Health != domain.HealthWarning {
		t.Fatalf("health = %q", row.Health)
	}
	if row.Fields["object"] != "Pod/api-78d9f" {
		t.Fatalf("object = %q", row.Fields["object"])
	}
}

func TestUnknownKindStillRendersIdentity(t *testing.T) {
	custom := object(t, `{"metadata": {"name": "my-thing", "namespace": "default"}}`)

	row := Row(domain.Kind("widgets.example.com"), custom)
	if row.Name != "my-thing" || row.Namespace != "default" {
		t.Fatalf("row = %+v; a custom resource must still list", row)
	}
	if row.Health != domain.HealthUnknown {
		t.Fatalf("health = %q", row.Health)
	}
}

func TestShortDurationUsesOneUnit(t *testing.T) {
	cases := map[string]string{
		"45s": ShortDuration(45_000_000_000),
		"3m":  ShortDuration(3 * 60_000_000_000),
		"5h":  ShortDuration(5 * 3600_000_000_000),
		"12d": ShortDuration(12 * 24 * 3600_000_000_000),
	}
	for want, got := range cases {
		if got != want {
			t.Fatalf("duration = %q, want %q", got, want)
		}
	}
}
