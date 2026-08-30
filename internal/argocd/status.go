package argocd

import (
	"sort"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// health reduces Argo CD's two status axes to the one traffic light every
// other row in the application wears.
//
// Health outranks sync, because a Degraded Application is degraded whatever
// Git says about it, and the sync status is the lesser of the two problems
// when both are present.
func health(sync, healthStatus string) domain.Health {
	switch normalise(healthStatus) {
	case "healthy":
		if normalise(sync) == "outofsync" {
			return domain.HealthWarning
		}
		return domain.HealthHealthy
	case "progressing":
		return domain.HealthProgress
	case "degraded":
		return domain.HealthCritical
	case "missing", "suspended":
		return domain.HealthWarning
	}

	// An Application whose health Argo CD has not reported still has a sync
	// status worth colouring, and an unreported health is not itself a fault.
	switch normalise(sync) {
	case "synced":
		return domain.HealthHealthy
	case "outofsync":
		return domain.HealthWarning
	default:
		return domain.HealthUnknown
	}
}

// attention decides whether an Application belongs in the Needs attention
// panel, and says why in the words the engineer will act on.
//
// Health outranks sync here too: an Application that is both degraded and out
// of sync is listed as degraded, because fixing the sync would not fix it.
func attention(app domain.ArgoApp, message string) (string, bool) {
	reason := ""
	switch normalise(app.HealthStatus) {
	case "degraded":
		reason = "Degraded"
	case "missing":
		reason = "Missing"
	default:
		if normalise(app.Sync) == "outofsync" {
			reason = "Out of sync"
		}
	}
	if reason == "" {
		return "", false
	}
	if message = strings.TrimSpace(message); message != "" {
		return message, true
	}
	return reason, true
}

// severity orders the Needs attention panel: what is broken before what is
// merely behind Git.
func severity(app domain.ArgoApp) int {
	switch normalise(app.HealthStatus) {
	case "degraded":
		return 0
	case "missing":
		return 1
	default:
		return 2
	}
}

// summarise counts every Application in the cluster and picks out the ones
// with a problem, in one pass over the objects.
func summarise(apps []*unstructured.Unstructured) (domain.ArgoSummary, []domain.ArgoApp) {
	summary := domain.ArgoSummary{Applications: len(apps)}
	var needsAttention []domain.ArgoApp

	for _, obj := range apps {
		app := describeApp(obj)

		switch normalise(app.Sync) {
		case "synced":
			summary.Synced++
		case "outofsync":
			summary.OutOfSync++
		}
		switch normalise(app.HealthStatus) {
		case "healthy":
			summary.Healthy++
		case "progressing":
			summary.Progressing++
		case "degraded":
			summary.Degraded++
		case "missing":
			summary.Missing++
		}

		if reason, wanted := attention(app, text(obj, "status", "health", "message")); wanted {
			app.Reason = reason
			needsAttention = append(needsAttention, app)
		}
	}

	sort.SliceStable(needsAttention, func(i, j int) bool {
		left, right := needsAttention[i], needsAttention[j]
		if severity(left) != severity(right) {
			return severity(left) < severity(right)
		}
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		return left.Name < right.Name
	})
	return summary, needsAttention
}

// describeApp reads the fields the dashboard and the action dialogs need out
// of one Application.
func describeApp(obj *unstructured.Unstructured) domain.ArgoApp {
	app := domain.ArgoApp{
		Namespace:    obj.GetNamespace(),
		Name:         obj.GetName(),
		Sync:         text(obj, "status", "sync", "status"),
		HealthStatus: text(obj, "status", "health", "status"),
		Project:      text(obj, "spec", "project"),
	}
	app.Health = health(app.Sync, app.HealthStatus)
	return app
}

// categorise files one Kubernetes event under the four things an engineer
// reading a timeline wants to tell apart.
//
// The reason is what Argo CD's controllers actually write, and it is read
// before the event type: a Warning is a failure whatever it is called, but a
// Normal event named OperationFailed is a failure too.
func categorise(eventType, reason string) domain.ArgoActivityKind {
	word := normalise(reason)
	switch {
	case contains(word, "fail", "error", "degrad", "unhealthy", "denied", "timeout"):
		return domain.ArgoActivityFailure
	case strings.EqualFold(strings.TrimSpace(eventType), "Warning"):
		return domain.ArgoActivityFailure
	case contains(word, "completed", "succeeded", "synced", "created", "healthy"):
		return domain.ArgoActivitySuccess
	case contains(word, "started", "progress", "syncing", "updated", "deleted", "pruned"):
		return domain.ArgoActivityProgress
	default:
		return domain.ArgoActivityInfo
	}
}

func contains(word string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(word, needle) {
			return true
		}
	}
	return false
}

// normalise folds the spelling differences between what Argo CD writes, what
// a chart templates, and what an older version wrote, so "OutOfSync",
// "outOfSync" and "out of sync" are one answer.
func normalise(value string) string {
	var builder strings.Builder
	for _, r := range strings.ToLower(value) {
		if r == ' ' || r == '-' || r == '_' {
			continue
		}
		builder.WriteRune(r)
	}
	return builder.String()
}

func text(obj *unstructured.Unstructured, fields ...string) string {
	value, _, _ := unstructured.NestedString(obj.Object, fields...)
	return value
}
