package argocd

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	k8sjson "k8s.io/apimachinery/pkg/util/json"

	"biebie-kube/internal/domain"
)

func application(t *testing.T, raw string) *unstructured.Unstructured {
	t.Helper()
	var content map[string]any
	if err := k8sjson.Unmarshal([]byte(raw), &content); err != nil {
		t.Fatalf("fixture: %v", err)
	}
	return &unstructured.Unstructured{Object: content}
}

func TestHealthOutranksSync(t *testing.T) {
	cases := []struct {
		name   string
		sync   string
		health string
		want   domain.Health
	}{
		{"synced and healthy", "Synced", "Healthy", domain.HealthHealthy},
		{"healthy but behind Git", "OutOfSync", "Healthy", domain.HealthWarning},
		{"degraded while synced", "Synced", "Degraded", domain.HealthCritical},
		{"degraded and out of sync", "OutOfSync", "Degraded", domain.HealthCritical},
		{"rolling out", "Synced", "Progressing", domain.HealthProgress},
		{"missing", "Synced", "Missing", domain.HealthWarning},
		{"paused", "Synced", "Suspended", domain.HealthWarning},
		{"health not reported yet", "Synced", "", domain.HealthHealthy},
		{"nothing reported yet", "", "", domain.HealthUnknown},
		{"spelled the other way", "outOfSync", "healthy", domain.HealthWarning},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := health(tc.sync, tc.health); got != tc.want {
				t.Fatalf("health(%q, %q) = %q, want %q", tc.sync, tc.health, got, tc.want)
			}
		})
	}
}

func TestAttentionNamesTheWorseOfTheTwoProblems(t *testing.T) {
	// Both axes are a problem. Fixing the sync would not fix a degraded
	// application, so degraded is what the panel must say.
	app := domain.ArgoApp{Sync: domain.ArgoOutOfSync, HealthStatus: domain.ArgoDegraded}
	reason, wanted := attention(app, "")
	if !wanted || reason != "Degraded" {
		t.Fatalf("reason = %q wanted = %v", reason, wanted)
	}

	// Argo CD's own message is more useful than the word it repeats.
	reason, _ = attention(app, "Deployment has 0/3 ready replicas")
	if reason != "Deployment has 0/3 ready replicas" {
		t.Fatalf("reason = %q; the controller's message should win", reason)
	}

	healthy := domain.ArgoApp{Sync: domain.ArgoSynced, HealthStatus: domain.ArgoHealthy}
	if _, wanted := attention(healthy, ""); wanted {
		t.Fatal("a synced, healthy application does not need attention")
	}

	// Suspended is deliberate, not a fault: an application somebody paused
	// must not sit in the panel demanding to be looked at.
	suspended := domain.ArgoApp{Sync: domain.ArgoSynced, HealthStatus: domain.ArgoSuspended}
	if _, wanted := attention(suspended, ""); wanted {
		t.Fatal("a suspended application is paused on purpose")
	}
}

func TestSummariseCountsBothAxesAndOrdersByWhatIsBroken(t *testing.T) {
	apps := []*unstructured.Unstructured{
		application(t, `{
			"metadata": {"name": "checkout", "namespace": "argocd"},
			"spec": {"project": "shop"},
			"status": {"sync": {"status": "Synced"}, "health": {"status": "Healthy"}}
		}`),
		application(t, `{
			"metadata": {"name": "billing", "namespace": "argocd"},
			"status": {"sync": {"status": "OutOfSync"}, "health": {"status": "Healthy"}}
		}`),
		application(t, `{
			"metadata": {"name": "search", "namespace": "argocd"},
			"status": {
				"sync": {"status": "OutOfSync"},
				"health": {"status": "Degraded", "message": "no healthy upstream"}
			}
		}`),
		application(t, `{
			"metadata": {"name": "ledger", "namespace": "argocd"},
			"status": {"sync": {"status": "Synced"}, "health": {"status": "Missing"}}
		}`),
		application(t, `{
			"metadata": {"name": "web", "namespace": "argocd"},
			"status": {"sync": {"status": "Synced"}, "health": {"status": "Progressing"}}
		}`),
	}

	summary, needsAttention := summarise(apps)

	want := domain.ArgoSummary{
		Applications: 5,
		Synced:       3,
		OutOfSync:    2,
		Healthy:      2,
		Progressing:  1,
		Degraded:     1,
		Missing:      1,
	}
	if summary != want {
		t.Fatalf("summary = %+v, want %+v", summary, want)
	}

	// Degraded, then missing, then merely behind Git.
	order := []string{"search", "ledger", "billing"}
	if len(needsAttention) != len(order) {
		t.Fatalf("needs attention = %d rows, want %d", len(needsAttention), len(order))
	}
	for i, name := range order {
		if needsAttention[i].Name != name {
			t.Fatalf("row %d = %q, want %q", i, needsAttention[i].Name, name)
		}
	}
	if needsAttention[0].Reason != "no healthy upstream" {
		t.Fatalf("reason = %q", needsAttention[0].Reason)
	}
}

func TestCategoriseReadsTheReasonBeforeTheEventType(t *testing.T) {
	cases := []struct {
		eventType string
		reason    string
		want      domain.ArgoActivityKind
	}{
		{"Warning", "SyncFailed", domain.ArgoActivityFailure},
		// A failure Argo CD wrote as an ordinary event is still a failure.
		{"Normal", "OperationFailed", domain.ArgoActivityFailure},
		{"Warning", "ResourceUpdated", domain.ArgoActivityFailure},
		{"Normal", "OperationCompleted", domain.ArgoActivitySuccess},
		{"Normal", "ResourceCreated", domain.ArgoActivitySuccess},
		{"Normal", "OperationStarted", domain.ArgoActivityProgress},
		{"Normal", "ResourceUpdated", domain.ArgoActivityProgress},
		{"Normal", "StatusRefreshed", domain.ArgoActivityInfo},
	}

	for _, tc := range cases {
		t.Run(tc.eventType+"/"+tc.reason, func(t *testing.T) {
			if got := categorise(tc.eventType, tc.reason); got != tc.want {
				t.Fatalf("categorise(%q, %q) = %q, want %q", tc.eventType, tc.reason, got, tc.want)
			}
		})
	}
}

func TestChipsLeaveOutWhatIsNotWrong(t *testing.T) {
	quiet := chipsFor(domain.ArgoSummary{Applications: 12, Synced: 12, Healthy: 12})
	if len(quiet) != 0 {
		t.Fatalf("chips = %+v; a healthy cluster earns no chips", quiet)
	}

	busy := chipsFor(domain.ArgoSummary{Applications: 4, Degraded: 1, OutOfSync: 2})
	if len(busy) != 2 {
		t.Fatalf("chips = %+v, want degraded and out of sync only", busy)
	}
	if busy[0].Label != "Degraded" || busy[0].Health != domain.HealthCritical {
		t.Fatalf("first chip = %+v", busy[0])
	}
}
