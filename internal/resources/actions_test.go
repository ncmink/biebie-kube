package resources

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// stamp is a fixed clock, so a patch and a job name are compared against a
// value rather than against whatever the test machine's clock said.
var stamp = time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)

func TestRestartPatchTouchesOnlyTheTemplateAnnotation(t *testing.T) {
	patch, err := restartPatch(stamp)
	if err != nil {
		t.Fatalf("restartPatch: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(patch, &decoded); err != nil {
		t.Fatalf("patch is not JSON: %v", err)
	}

	// A merge patch says only what changes. Anything else in here would be
	// this application overwriting a field the manifest's owner set.
	spec, ok := decoded["spec"].(map[string]any)
	if !ok || len(decoded) != 1 || len(spec) != 1 {
		t.Fatalf("patch reaches beyond the template: %s", patch)
	}

	annotations := spec["template"].(map[string]any)["metadata"].(map[string]any)["annotations"].(map[string]any)
	if got := annotations[restartedAt]; got != "2026-08-30T12:00:00Z" {
		t.Fatalf("stamp = %v", got)
	}
}

func TestRestartPatchChangesOnEveryPress(t *testing.T) {
	first, _ := restartPatch(stamp)
	second, _ := restartPatch(stamp.Add(time.Second))

	// Two restarts a second apart must produce two different templates. An
	// identical patch is a no-op the controller ignores, which would look like
	// the button did nothing.
	if string(first) == string(second) {
		t.Fatal("a second restart produced the same patch, so no rollout would happen")
	}
}

func TestManualJobNameFitsTheLabelLimit(t *testing.T) {
	long := strings.Repeat("nightly-reconciliation-", 4)
	name := manualJobName(long, stamp)

	if len(name) > maxJobName {
		t.Fatalf("name is %d characters: %q", len(name), name)
	}
	if !strings.Contains(name, "-manual-") {
		t.Fatalf("a manual run is not marked as one: %q", name)
	}
	if strings.Contains(name, "--") {
		t.Fatalf("truncation left a doubled separator: %q", name)
	}
}

func TestManualJobNameKeepsShortNamesWhole(t *testing.T) {
	if got := manualJobName("backup", stamp); got != "backup-manual-1788091200" {
		t.Fatalf("name = %q", got)
	}
}

func cronJobFixture(t *testing.T) *unstructured.Unstructured {
	t.Helper()
	return object(t, `{
		"apiVersion": "batch/v1",
		"kind": "CronJob",
		"metadata": {"name": "backup", "namespace": "ops", "uid": "cron-uid"},
		"spec": {
			"schedule": "0 2 * * *",
			"suspend": true,
			"jobTemplate": {
				"metadata": {"labels": {"app": "backup"}},
				"spec": {"template": {"spec": {"containers": [{"name": "backup", "image": "backup:1"}]}}}
			}
		}
	}`)
}

func TestJobFromCronJobCarriesTheTemplateAndItsLabels(t *testing.T) {
	job, err := jobFromCronJob(cronJobFixture(t), builtin(t, domain.KindJob), stamp)
	if err != nil {
		t.Fatalf("jobFromCronJob: %v", err)
	}

	if job.GetAPIVersion() != "batch/v1" || job.GetKind() != "Job" {
		t.Fatalf("job is %s/%s", job.GetAPIVersion(), job.GetKind())
	}
	if job.GetNamespace() != "ops" {
		t.Fatalf("namespace = %q", job.GetNamespace())
	}
	if job.GetLabels()["app"] != "backup" {
		t.Fatalf("the template's labels were dropped: %v", job.GetLabels())
	}
	if job.GetAnnotations()[instantiate] != "manual" {
		t.Fatalf("the run is not marked manual: %v", job.GetAnnotations())
	}

	containers, found, err := unstructured.NestedSlice(job.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || len(containers) != 1 {
		t.Fatalf("the job template's own spec did not come along: %v", job.Object["spec"])
	}
}

func TestJobFromCronJobLeavesTheRunUnowned(t *testing.T) {
	job, err := jobFromCronJob(cronJobFixture(t), builtin(t, domain.KindJob), stamp)
	if err != nil {
		t.Fatalf("jobFromCronJob: %v", err)
	}

	// An owner reference would enrol a manual run in the cron job's history
	// limits and in a Forbid concurrency policy, which is how triggering a
	// suspended job by hand could block the schedule it was meant to stand in
	// for.
	if refs := job.GetOwnerReferences(); len(refs) != 0 {
		t.Fatalf("the job is owned by %+v", refs)
	}
}

func TestJobFromCronJobRefusesACronJobWithNoTemplate(t *testing.T) {
	empty := object(t, `{"metadata": {"name": "backup", "namespace": "ops"}, "spec": {"schedule": "0 2 * * *"}}`)

	if _, err := jobFromCronJob(empty, builtin(t, domain.KindJob), stamp); err == nil {
		t.Fatal("a cron job with no job template cannot be triggered")
	}
}

func TestScalableKindsAreOnlyTheOnesWithAScaleSubresource(t *testing.T) {
	// A daemon set runs one pod per matching node and has no scale
	// subresource: offering the action would produce a 404 from the API server
	// at the moment the user pressed the button.
	if builtin(t, domain.KindDaemonSet).Supports(domain.ActionScale) {
		t.Fatal("daemon sets cannot be scaled")
	}
	if !builtin(t, domain.KindDeployment).Supports(domain.ActionScale) {
		t.Fatal("deployments can be scaled")
	}
}

func TestCustomKindsOfferNoActions(t *testing.T) {
	custom := domain.KindInfo{Kind: domain.CustomKind("widgets", "example.com"), Custom: true}

	for _, action := range []domain.ResourceAction{
		domain.ActionScale, domain.ActionRestart, domain.ActionCordon,
		domain.ActionUncordon, domain.ActionSuspend, domain.ActionResume,
		domain.ActionTrigger,
	} {
		if custom.Supports(action) {
			t.Fatalf("an operator's resource was offered %q", action)
		}
	}
}
