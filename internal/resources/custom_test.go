package resources

import (
	"testing"

	"biebie-kube/internal/domain"
)

// argoApplication is the entry a cluster running Argo CD produces, with the
// columns its definition declares.
func argoApplication() domain.KindInfo {
	return domain.KindInfo{
		Kind:     domain.CustomKind("applications", "argoproj.io"),
		Title:    "Applications",
		Category: domain.CategoryCustom,
		Group:    "argoproj.io", Version: "v1alpha1", Resource: "applications",
		Namespaced: true,
		Custom:     true,
		Columns: []domain.Column{
			{Key: "syncStatus", Title: "Sync Status", Path: ".status.sync.status"},
			{Key: "healthStatus", Title: "Health Status", Path: ".status.health.status"},
		},
	}
}

func TestCustomResourceFillsItsDeclaredColumns(t *testing.T) {
	app := object(t, `{
		"metadata": {"name": "checkout", "namespace": "argocd"},
		"status": {
			"sync": {"status": "Synced"},
			"health": {"status": "Healthy"}
		}
	}`)

	row := Row(argoApplication(), app)

	if row.Fields["syncStatus"] != "Synced" {
		t.Fatalf("syncStatus = %q", row.Fields["syncStatus"])
	}
	if row.Fields["healthStatus"] != "Healthy" {
		t.Fatalf("healthStatus = %q", row.Fields["healthStatus"])
	}
}

func TestCustomResourceTakesHealthFromItsOwnColumn(t *testing.T) {
	// An Argo CD Application reports no conditions and no phase. Its health
	// lives only in the column its definition declares.
	healthy := object(t, `{
		"metadata": {"name": "checkout", "namespace": "argocd"},
		"status": {"sync": {"status": "Synced"}, "health": {"status": "Healthy"}}
	}`)
	if row := Row(argoApplication(), healthy); row.Health != domain.HealthHealthy {
		t.Fatalf("health = %q status = %q", row.Health, row.Status)
	}

	degraded := object(t, `{
		"metadata": {"name": "checkout", "namespace": "argocd"},
		"status": {"sync": {"status": "Synced"}, "health": {"status": "Degraded"}}
	}`)
	row := Row(argoApplication(), degraded)
	if row.Health != domain.HealthCritical {
		t.Fatalf("health = %q; a synced application can still be degraded", row.Health)
	}
	if row.Status != "Degraded" {
		t.Fatalf("status = %q", row.Status)
	}
}

func TestConditionsWinOverAColumn(t *testing.T) {
	// The column says healthy, the resource says it is not. The resource's own
	// conditions are the stronger statement and must not be papered over.
	app := object(t, `{
		"metadata": {"name": "checkout"},
		"status": {
			"health": {"status": "Healthy"},
			"conditions": [{"type": "Ready", "status": "False", "reason": "ImagePullFailed"}]
		}
	}`)

	row := Row(argoApplication(), app)

	if row.Health != domain.HealthCritical || row.Status != "ImagePullFailed" {
		t.Fatalf("health = %q status = %q", row.Health, row.Status)
	}
}

func TestCustomResourceWithoutStatusYetRendersEmptyCells(t *testing.T) {
	app := object(t, `{"metadata": {"name": "just-created", "namespace": "argocd"}}`)

	row := Row(argoApplication(), app)

	if row.Name != "just-created" {
		t.Fatalf("name = %q", row.Name)
	}
	if row.Fields["syncStatus"] != "" {
		t.Fatalf("syncStatus = %q; a controller that has not run yet is an empty cell", row.Fields["syncStatus"])
	}
	if row.Health != domain.HealthUnknown {
		t.Fatalf("health = %q", row.Health)
	}
}

func TestCustomResourceNumbersAreNotRenderedAsFloats(t *testing.T) {
	info := domain.KindInfo{
		Kind: domain.CustomKind("widgets", "example.com"), Custom: true,
		Columns: []domain.Column{
			{Key: "replicas", Title: "Replicas", Path: ".spec.replicas"},
			{Key: "paused", Title: "Paused", Path: ".spec.paused"},
		},
	}
	widget := object(t, `{"metadata": {"name": "w"}, "spec": {"replicas": 3, "paused": true}}`)

	row := Row(info, widget)

	if row.Fields["replicas"] != "3" {
		t.Fatalf("replicas = %q", row.Fields["replicas"])
	}
	if row.Fields["paused"] != "True" {
		t.Fatalf("paused = %q", row.Fields["paused"])
	}
}

func TestCustomHealthReadsConditions(t *testing.T) {
	ready := object(t, `{"status": {"conditions": [{"type": "Ready", "status": "True"}]}}`)
	if health, _ := customHealth(ready); health != domain.HealthHealthy {
		t.Fatalf("ready health = %q", health)
	}

	notReady := object(t, `{"status": {"conditions": [
		{"type": "Ready", "status": "False", "reason": "BackendUnavailable"}
	]}}`)
	health, status := customHealth(notReady)
	if health != domain.HealthCritical {
		t.Fatalf("health = %q", health)
	}
	if status != "BackendUnavailable" {
		t.Fatalf("status = %q; the controller's own reason is what the engineer acts on", status)
	}

	degraded := object(t, `{"status": {"conditions": [
		{"type": "Ready", "status": "True"},
		{"type": "Degraded", "status": "True", "reason": "QuorumLost"}
	]}}`)
	if health, status := customHealth(degraded); health != domain.HealthCritical || status != "QuorumLost" {
		t.Fatalf("health = %q status = %q; a degraded resource is not healthy because it is also ready",
			health, status)
	}

	pending := object(t, `{"status": {"conditions": [{"type": "Ready", "status": "Unknown"}]}}`)
	if health, _ := customHealth(pending); health != domain.HealthProgress {
		t.Fatalf("unknown-condition health = %q", health)
	}
}

func TestCustomHealthFallsBackToPhase(t *testing.T) {
	running := object(t, `{"status": {"phase": "Running"}}`)
	if health, status := customHealth(running); health != domain.HealthHealthy || status != "Running" {
		t.Fatalf("health = %q status = %q", health, status)
	}

	failed := object(t, `{"status": {"phase": "Failed"}}`)
	if health, _ := customHealth(failed); health != domain.HealthCritical {
		t.Fatalf("failed health = %q", health)
	}

	odd := object(t, `{"status": {"phase": "Reticulating"}}`)
	if health, status := customHealth(odd); health != domain.HealthUnknown || status != "Reticulating" {
		t.Fatalf("health = %q status = %q; an unrecognised phase is shown without a verdict",
			health, status)
	}
}

func TestDefinitionsRowMarksTheStoredVersion(t *testing.T) {
	crd := object(t, `{
		"metadata": {"name": "applications.argoproj.io"},
		"spec": {
			"group": "argoproj.io",
			"scope": "Namespaced",
			"names": {"kind": "Application"},
			"versions": [
				{"name": "v1alpha1", "served": true, "storage": true},
				{"name": "v1beta1", "served": true, "storage": false},
				{"name": "v1", "served": false, "storage": false}
			]
		}
	}`)

	row := Row(builtin(t, domain.KindCustomResourceDefinition), crd)

	if row.Fields["group"] != "argoproj.io" || row.Fields["kind"] != "Application" {
		t.Fatalf("fields = %+v", row.Fields)
	}
	if row.Fields["scope"] != "Namespaced" {
		t.Fatalf("scope = %q", row.Fields["scope"])
	}
	if row.Fields["versions"] != "v1alpha1* v1beta1" {
		t.Fatalf("versions = %q; only served versions are listed and the stored one is marked",
			row.Fields["versions"])
	}
}
