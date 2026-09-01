package gitops

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

const replicaSource = `
apiVersion: apps/v1
kind: Deployment
metadata: {name: super-report, namespace: reports}
spec: {selector: {matchLabels: {app: super-report}}}
`

func TestOmittedReplicasEqualToTheDefaultAreNotADifference(t *testing.T) {
	live := deploymentYAML(t, 1, "")
	found := classify(differencesBetween(t, replicaSource, live, false))
	for _, difference := range found {
		if difference.Path == "spec.replicas" {
			t.Fatalf("omitted replicas against the default leaked as %+v", difference)
		}
	}
}

func TestOmittedReplicasAgainstANonDefaultAreOmittedNotDeclared(t *testing.T) {
	// The repository did not ask for 1 replica. Kubernetes would have used 1
	// had nothing else written the field, and that default is carried beside
	// the omission rather than as Git's value.
	search := domain.ManifestSearch{
		Certainty: domain.ManifestExact,
		Located:   &domain.ManifestLocation{Path: "deploy.yaml", Content: replicaSource},
	}
	got := compareAgainst(search, subjectFor(t, liveReplicas(4), false), refusal{})
	if got.Meaningful != 1 || len(got.Differences) == 0 {
		t.Fatalf("comparison = %+v", got)
	}
	difference := got.Differences[0]
	if difference.Path != "spec.replicas" {
		t.Fatalf("path = %q", difference.Path)
	}
	if !difference.SourceImplicit || difference.Source != "1" || difference.Live != "4" {
		t.Fatalf("source = %q implicit=%v live=%q", difference.Source, difference.SourceImplicit, difference.Live)
	}
	if difference.Explanation != nil {
		t.Fatalf("an unexplained difference carried an explanation: %+v", difference.Explanation)
	}
}

func TestAMatchingHPAExplainsReplicaDrift(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		hpas: []*unstructured.Unstructured{hpaYAML(t, "super-report", "super-report", "reports", 2, 10, 4, 4)},
	})
	got := explained[0].Explanation
	if got == nil {
		t.Fatal("a matching HPA produced no explanation")
	}
	if got.Cause != domain.CauseController || got.Confidence != domain.ConfidenceConfirmed {
		t.Fatalf("cause = %q confidence = %q", got.Cause, got.Confidence)
	}
	if got.ManagedBy != "HorizontalPodAutoscaler / super-report" {
		t.Fatalf("managedBy = %q", got.ManagedBy)
	}
	if !hasEvidence(got, domain.EvidenceHPATarget, "super-report") {
		t.Fatalf("missing HPA evidence: %+v", got.Evidence)
	}
	if hasEvidence(got, domain.EvidenceArgoIgnore, "") {
		t.Fatalf("claimed an ignore rule that was not there: %+v", got.Evidence)
	}
	if got.Attention != domain.AttentionNone {
		t.Fatalf("attention = %q, want none: an HPA is a finding, not an alarm", got.Attention)
	}
	if got.Summary != "HPA explains the live replica count." {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func TestAnExplicitSourceReplicaCountIsStillExplainedByAHPA(t *testing.T) {
	difference := replicaDiff(t, "2", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		hpas: []*unstructured.Unstructured{hpaYAML(t, "super-report", "super-report", "reports", 2, 10, 4, 4)},
	})
	got := explained[0]
	if got.Source != "2" || got.Live != "4" || got.SourceImplicit {
		t.Fatalf("the declared source was rewritten: %+v", got)
	}
	if got.Explanation == nil || got.Explanation.Cause != domain.CauseController {
		t.Fatalf("explanation = %+v", got.Explanation)
	}
}

func TestHPAEvidenceCarriesMinMaxCurrentAndDesired(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		hpas: []*unstructured.Unstructured{hpaYAML(t, "super-report", "super-report", "reports", 2, 10, 4, 4)},
	})
	facts := evidenceFacts(explained[0].Explanation, domain.EvidenceHPATarget)
	for name, want := range map[string]string{"Min": "2", "Max": "10", "Current": "4", "Desired": "4"} {
		if facts[name] != want {
			t.Fatalf("%s = %q, want %q (facts=%v)", name, facts[name], want, facts)
		}
	}
}

func TestAnHPATargetingAnotherDeploymentDoesNotExplainThisOne(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		hpas: []*unstructured.Unstructured{hpaYAML(t, "other", "other", "reports", 2, 10, 4, 4)},
	})
	if explained[0].Explanation != nil {
		t.Fatalf("explained by the wrong HPA: %+v", explained[0].Explanation)
	}
}

func TestAnHPAInAnotherNamespaceDoesNotExplainThisDeployment(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		hpas: []*unstructured.Unstructured{hpaYAML(t, "super-report", "super-report", "elsewhere", 2, 10, 4, 4)},
	})
	if explained[0].Explanation != nil {
		t.Fatalf("explained by an HPA in another namespace: %+v", explained[0].Explanation)
	}
}

func TestAnApplicableArgoIgnoreRuleIsReturned(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		app:  applicationYAML(t, ignoreEntry("apps", "Deployment", "", "", []string{"/spec/replicas"}, nil)),
	})
	got := explained[0].Explanation
	if got == nil || got.Cause != domain.CauseArgoIgnored {
		t.Fatalf("explanation = %+v", got)
	}
	if !hasEvidence(got, domain.EvidenceArgoIgnore, "/spec/replicas") {
		t.Fatalf("missing ignore evidence: %+v", got.Evidence)
	}
	if got.Confidence != domain.ConfidenceUnknown {
		t.Fatalf("confidence = %q: an ignore rule does not identify who writes the value", got.Confidence)
	}
}

func TestAnIgnoreRuleForAnotherResourceDoesNotApply(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		app:  applicationYAML(t, ignoreEntry("apps", "Deployment", "other", "", []string{"/spec/replicas"}, nil)),
	})
	if explained[0].Explanation != nil {
		t.Fatalf("applied another object's ignore rule: %+v", explained[0].Explanation)
	}
}

func TestSyncedIsExpectedWhenAnIgnoreRuleAppliesBesideAHPA(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		hpas: []*unstructured.Unstructured{hpaYAML(t, "super-report", "super-report", "reports", 2, 10, 4, 4)},
		app:  applicationYAML(t, ignoreEntry("apps", "Deployment", "", "", []string{"/spec/replicas"}, nil)),
	})
	got := explained[0].Explanation
	if got == nil {
		t.Fatal("no explanation")
	}
	if got.Cause != domain.CauseController || got.Attention != domain.AttentionNone {
		t.Fatalf("cause = %q attention = %q", got.Cause, got.Attention)
	}
	if got.ApplicationIgnore != "applies" {
		t.Fatalf("applicationIgnore = %q", got.ApplicationIgnore)
	}
	if !strings.Contains(got.Note, "Synced") {
		t.Fatalf("note does not say why Synced is possible: %q", got.Note)
	}
	if strings.Contains(got.Summary, "Synced") {
		t.Fatalf("summary buried the HPA under Argo CD: %q", got.Summary)
	}
}

func TestAHPAWithoutAnIgnoreRuleDoesNotClaimArgoIgnoresReplicas(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		hpas: []*unstructured.Unstructured{hpaYAML(t, "super-report", "super-report", "reports", 2, 10, 4, 4)},
		app:  applicationYAML(t),
	})
	got := explained[0].Explanation
	if got == nil || got.Cause != domain.CauseController {
		t.Fatalf("explanation = %+v", got)
	}
	if hasEvidence(got, domain.EvidenceArgoIgnore, "") {
		t.Fatalf("claimed an ignore: %+v", got.Evidence)
	}
	if got.ApplicationIgnore != "absent" {
		t.Fatalf("applicationIgnore = %q, want absent", got.ApplicationIgnore)
	}
	if strings.Contains(got.Summary, "does not appear to ignore") ||
		strings.Contains(got.Summary, "contention") {
		t.Fatalf("summary overclaimed what Argo CD does: %q", got.Summary)
	}
	if !strings.Contains(got.Note, "Application-level") {
		t.Fatalf("note = %q", got.Note)
	}
	if strings.Contains(strings.ToLower(got.Note), "ignores nothing") ||
		strings.Contains(got.Note, "does not ignore") {
		t.Fatalf("treated a missing Application rule as proof Argo ignores nothing: %q", got.Note)
	}
}

func TestAnIgnoreRuleWithoutAHPALeavesTheControllerUnknown(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		app:  applicationYAML(t, ignoreEntry("apps", "Deployment", "", "", []string{"/spec/replicas"}, nil)),
	})
	got := explained[0].Explanation
	if got == nil || got.Cause != domain.CauseArgoIgnored {
		t.Fatalf("explanation = %+v", got)
	}
	if got.ManagedBy != "" {
		t.Fatalf("named a controller that was not found: %q", got.ManagedBy)
	}
	if !strings.Contains(got.Summary, "no controller") {
		t.Fatalf("summary = %q", got.Summary)
	}
}

func TestNeitherHPANorIgnoreLeavesTheDifferenceUnexplained(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		hpas: []*unstructured.Unstructured{hpaYAML(t, "other", "other", "reports", 1, 5, 2, 2)},
		app:  applicationYAML(t, ignoreEntry("apps", "StatefulSet", "", "", []string{"/spec/replicas"}, nil)),
	})
	if explained[0].Explanation != nil {
		t.Fatalf("invented a cause: %+v", explained[0].Explanation)
	}
}

func TestManagedFieldsOwnershipIsSupportingEvidence(t *testing.T) {
	live := withManager(t, deploymentYAML(t, 4, ""), "horizontal-pod-autoscaler", "spec.replicas")
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: live,
		hpas: []*unstructured.Unstructured{hpaYAML(t, "super-report", "super-report", "reports", 2, 10, 4, 4)},
	})
	got := explained[0].Explanation
	if !hasEvidence(got, domain.EvidenceFieldOwner, "horizontal-pod-autoscaler") {
		t.Fatalf("missing field-owner evidence: %+v", got)
	}
	if evidenceConfidence(got, domain.EvidenceFieldOwner) != domain.ConfidenceSupporting {
		t.Fatalf("managedFields was treated as confirmed ownership")
	}
}

func TestUnrelatedManagedFieldsDoNotExplainReplicas(t *testing.T) {
	live := withManager(t, deploymentYAML(t, 4, ""), "kube-controller-manager", "spec.template")
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: live,
	})
	got := explained[0].Explanation
	if got != nil && hasEvidence(got, domain.EvidenceFieldOwner, "kube-controller-manager") {
		t.Fatalf("an unrelated manager explained replicas: %+v", got)
	}
}

func TestAnUnknownManagerIsNotTurnedIntoAGuess(t *testing.T) {
	live := withManager(t, deploymentYAML(t, 4, ""), "kubectl-client-side-apply", "spec.replicas")
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{live: live})
	got := explained[0].Explanation
	if got == nil {
		t.Fatal("supporting evidence was dropped")
	}
	if got.Cause != domain.CauseUnknown {
		t.Fatalf("cause = %q: a manager name is not a controller", got.Cause)
	}
	if strings.Contains(strings.ToLower(got.Summary), "manually") ||
		strings.Contains(strings.ToLower(got.Summary), "user") {
		t.Fatalf("guessed a person from a manager name: %q", got.Summary)
	}
}

func TestMalformedManagedFieldsDoNotBreakAnalysis(t *testing.T) {
	live := deploymentYAML(t, 4, "")
	_ = unstructured.SetNestedSlice(live.Object, []any{"not-an-entry"}, "metadata", "managedFields")
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: live,
		hpas: []*unstructured.Unstructured{hpaYAML(t, "super-report", "super-report", "reports", 2, 10, 4, 4)},
	})
	if explained[0].Explanation == nil || explained[0].Explanation.Cause != domain.CauseController {
		t.Fatalf("malformed managedFields hid the HPA: %+v", explained[0].Explanation)
	}
}

func TestHPAQueryFailureLeavesTheDifferenceAndMarksAnalysisUnchecked(t *testing.T) {
	search := domain.ManifestSearch{
		Certainty: domain.ManifestExact,
		Located:   &domain.ManifestLocation{Path: "deploy.yaml", Content: replicaSource},
	}
	comparison := compareAgainst(search, subjectFor(t, liveReplicas(4), false), refusal{})
	if comparison.State != domain.ComparisonDiffers {
		t.Fatalf("state = %q", comparison.State)
	}

	got := enrich(comparison, analysis{
		live:   deploymentYAML(t, 4, ""),
		hpaErr: errors.New("forbidden"),
	})
	if got.State != domain.ComparisonDiffers {
		t.Fatalf("analysis failure changed the comparison state to %q", got.State)
	}
	if got.Meaningful != 1 {
		t.Fatalf("the difference was dropped: %+v", got)
	}
	explanation := got.Differences[0].Explanation
	if explanation == nil {
		t.Fatal("a failed lookup produced no record of the failure")
	}
	if explanation.Cause != domain.CauseUnknown {
		t.Fatalf("cause = %q", explanation.Cause)
	}
	if len(explanation.Unchecked) == 0 {
		t.Fatal("the failure was not recorded as unchecked")
	}
}

func TestArgoAnalysisFailureLeavesTheHPAExplanation(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live:    deploymentYAML(t, 4, ""),
		hpas:    []*unstructured.Unstructured{hpaYAML(t, "super-report", "super-report", "reports", 2, 10, 4, 4)},
		argoErr: errors.New("forbidden"),
	})
	got := explained[0].Explanation
	if got == nil || got.Cause != domain.CauseController {
		t.Fatalf("lost the HPA because Argo could not be read: %+v", got)
	}
	if hasEvidence(got, domain.EvidenceArgoIgnore, "") {
		t.Fatalf("claimed an ignore after a failed read: %+v", got.Evidence)
	}
	if len(got.Unchecked) == 0 {
		t.Fatal("the Argo failure was not recorded")
	}
	if strings.Contains(got.Summary, "does not appear to ignore") {
		t.Fatalf("treated a failed read as an absent rule: %q", got.Summary)
	}
	if got.ApplicationIgnore != "unread" {
		t.Fatalf("applicationIgnore = %q, want unread", got.ApplicationIgnore)
	}
}

func TestASimpleJQPathExpressionIsTreatedAsAnIgnore(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		app:  applicationYAML(t, ignoreEntry("apps", "Deployment", "", "", nil, []string{".spec.replicas"})),
	})
	if explained[0].Explanation == nil || explained[0].Explanation.Cause != domain.CauseArgoIgnored {
		t.Fatalf("explanation = %+v", explained[0].Explanation)
	}
}

func TestAnUnevaluatedJQExpressionIsRecordedRatherThanInvented(t *testing.T) {
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		app: applicationYAML(t, ignoreEntry("apps", "Deployment", "", "", nil,
			[]string{`.spec.replicas | select(. > 1)`})),
	})
	got := explained[0].Explanation
	if got != nil && got.Cause == domain.CauseArgoIgnored {
		t.Fatalf("evaluated jq that this slice cannot read: %+v", got)
	}
	if got == nil || len(got.Unchecked) == 0 {
		t.Fatalf("the unevaluated expression vanished: %+v", got)
	}
}

func TestAnHPAWithTheWrongAPIGroupDoesNotMatch(t *testing.T) {
	hpa := hpaYAML(t, "super-report", "super-report", "reports", 2, 10, 4, 4)
	_ = unstructured.SetNestedField(hpa.Object, "v1", "spec", "scaleTargetRef", "apiVersion")
	difference := replicaDiff(t, "", 4)
	explained := analyse([]domain.StateDifference{difference}, analysis{
		live: deploymentYAML(t, 4, ""),
		hpas: []*unstructured.Unstructured{hpa},
	})
	if explained[0].Explanation != nil {
		t.Fatalf("a core v1 scale target explained an apps Deployment: %+v", explained[0].Explanation)
	}
}

func TestEnrichCountsAnExplainedReplicaDifference(t *testing.T) {
	search := domain.ManifestSearch{
		Certainty: domain.ManifestExact,
		Located:   &domain.ManifestLocation{Path: "deploy.yaml", Content: replicaSource},
	}
	comparison := compareAgainst(search, subjectFor(t, liveReplicas(4), false), refusal{})
	got := enrich(comparison, analysis{
		live: deploymentYAML(t, 4, ""),
		hpas: []*unstructured.Unstructured{hpaYAML(t, "super-report", "super-report", "reports", 2, 10, 4, 4)},
		app:  applicationYAML(t, ignoreEntry("apps", "Deployment", "", "", []string{"/spec/replicas"}, nil)),
	})
	if got.Explained != 1 || got.NeedsAttention != 0 {
		t.Fatalf("explained = %d needsAttention = %d", got.Explained, got.NeedsAttention)
	}
	if !strings.Contains(got.Reason, "controller-managed") {
		t.Fatalf("reason = %q", got.Reason)
	}
}

func TestAManagedFieldsManagerIgnoreAppliesOnlyWhenThatManagerOwnsTheField(t *testing.T) {
	rule := ignoreEntry("apps", "Deployment", "", "", nil, nil)
	rule["managedFieldsManagers"] = []any{"horizontal-pod-autoscaler"}

	owned := withManager(t, deploymentYAML(t, 4, ""), "horizontal-pod-autoscaler", "spec.replicas")
	explained := analyse([]domain.StateDifference{replicaDiff(t, "", 4)}, analysis{
		live: owned,
		app:  applicationYAML(t, rule),
	})
	if explained[0].Explanation == nil || explained[0].Explanation.Cause != domain.CauseArgoIgnored {
		t.Fatalf("a manager-owned field was not ignored: %+v", explained[0].Explanation)
	}

	other := withManager(t, deploymentYAML(t, 4, ""), "kube-controller-manager", "spec.replicas")
	missed := analyse([]domain.StateDifference{replicaDiff(t, "", 4)}, analysis{
		live: other,
		app:  applicationYAML(t, rule),
	})
	if missed[0].Explanation != nil && missed[0].Explanation.Cause == domain.CauseArgoIgnored {
		t.Fatalf("an ignore for a different manager applied: %+v", missed[0].Explanation)
	}
}

func TestEnrichDoesNotTurnAComparisonUnavailable(t *testing.T) {
	got := enrich(unavailable(domain.BlockerGenerated, "Helm renders this."), analysis{
		hpaErr: errors.New("forbidden"),
	})
	if got.State != domain.ComparisonUnavailable || got.Blocker != domain.BlockerGenerated {
		t.Fatalf("enrichment changed an unavailable comparison: %+v", got)
	}
}

func replicaDiff(t *testing.T, source string, live int) domain.StateDifference {
	t.Helper()
	difference := domain.StateDifference{
		Path:  "spec.replicas",
		Kind:  domain.DifferenceChanged,
		Class: domain.DifferenceMeaningful,
		Label: "Replicas",
		Live:  itoa(live),
	}
	if source == "" {
		difference.Kind = domain.DifferenceAddedInLive
		difference.Source = "1"
		difference.SourceImplicit = true
	} else {
		difference.Source = source
	}
	return difference
}

func deploymentYAML(t *testing.T, replicas int, extra string) *unstructured.Unstructured {
	t.Helper()
	return object(t, liveReplicas(replicas)+extra)
}

func liveReplicas(replicas int) string {
	return `
apiVersion: apps/v1
kind: Deployment
metadata: {name: super-report, namespace: reports}
spec:
  replicas: ` + itoa(replicas) + `
  selector: {matchLabels: {app: super-report}}
`
}

func hpaYAML(t *testing.T, name, target, namespace string, min, max, current, desired int) *unstructured.Unstructured {
	t.Helper()
	return object(t, `
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata: {name: `+name+`, namespace: `+namespace+`}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: `+target+`
  minReplicas: `+itoa(min)+`
  maxReplicas: `+itoa(max)+`
status:
  currentReplicas: `+itoa(current)+`
  desiredReplicas: `+itoa(desired)+`
`)
}

func ignoreEntry(group, kind, name, namespace string, pointers, jq []string) map[string]any {
	entry := map[string]any{"group": group, "kind": kind}
	if name != "" {
		entry["name"] = name
	}
	if namespace != "" {
		entry["namespace"] = namespace
	}
	if len(pointers) > 0 {
		list := make([]any, len(pointers))
		for i, pointer := range pointers {
			list[i] = pointer
		}
		entry["jsonPointers"] = list
	}
	if len(jq) > 0 {
		list := make([]any, len(jq))
		for i, expr := range jq {
			list[i] = expr
		}
		entry["jqPathExpressions"] = list
	}
	return entry
}

func applicationYAML(t *testing.T, ignores ...map[string]any) *unstructured.Unstructured {
	t.Helper()
	app := object(t, `
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata: {name: reports, namespace: argocd}
spec: {destination: {namespace: reports}}
`)
	if len(ignores) == 0 {
		return app
	}
	list := make([]any, len(ignores))
	for i, ignore := range ignores {
		list[i] = ignore
	}
	if err := unstructured.SetNestedSlice(app.Object, list, "spec", "ignoreDifferences"); err != nil {
		t.Fatal(err)
	}
	return app
}

func withManager(t *testing.T, live *unstructured.Unstructured, manager, path string) *unstructured.Unstructured {
	t.Helper()
	keys := fieldKeys(path)
	tree := map[string]any{}
	current := tree
	for index, key := range keys {
		if index == len(keys)-1 {
			current[key] = map[string]any{}
			break
		}
		next := map[string]any{}
		current[key] = next
		current = next
	}
	raw, err := json.Marshal(tree)
	if err != nil {
		t.Fatal(err)
	}
	live.SetManagedFields([]metav1.ManagedFieldsEntry{{
		Manager:    manager,
		Operation:  metav1.ManagedFieldsOperationUpdate,
		APIVersion: "apps/v1",
		FieldsType: "FieldsV1",
		FieldsV1:   &metav1.FieldsV1{Raw: raw},
	}})
	return live
}

func hasEvidence(explanation *domain.DifferenceExplanation, kind domain.EvidenceKind, subject string) bool {
	if explanation == nil {
		return false
	}
	for _, item := range explanation.Evidence {
		if item.Kind != kind {
			continue
		}
		if subject == "" || item.Subject == subject {
			return true
		}
	}
	return false
}

func evidenceConfidence(explanation *domain.DifferenceExplanation, kind domain.EvidenceKind) domain.ExplanationConfidence {
	if explanation == nil {
		return ""
	}
	for _, item := range explanation.Evidence {
		if item.Kind == kind {
			return item.Confidence
		}
	}
	return ""
}

func evidenceFacts(explanation *domain.DifferenceExplanation, kind domain.EvidenceKind) map[string]string {
	out := map[string]string{}
	if explanation == nil {
		return out
	}
	for _, item := range explanation.Evidence {
		if item.Kind != kind {
			continue
		}
		for _, fact := range item.Facts {
			out[fact.Name] = fact.Value
		}
	}
	return out
}
