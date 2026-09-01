package gitops

import (
	"encoding/json"
	"fmt"
	"strconv"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// analysis is the cluster state one comparison consults, gathered once so
// several differences on the same object do not each list HPAs.
//
// Failures are carried rather than returned. The comparison has already
// happened by the time this exists, and an HPA list that was forbidden must
// not take that answer away.
type analysis struct {
	live    *unstructured.Unstructured
	hpas    []*unstructured.Unstructured
	app     *unstructured.Unstructured
	hpaErr  error
	argoErr error
}

// enrich attaches cluster evidence to an already-computed comparison.
//
// The comparison is the authority. This may add an explanation, recount what
// is explained, and rewrite the summary sentence; it may not change the state
// to unavailable or drop a difference.
func enrich(comparison domain.StateComparison, state analysis) domain.StateComparison {
	if comparison.State == domain.ComparisonUnavailable || len(comparison.Differences) == 0 {
		return comparison
	}
	counted := tally(analyse(comparison.Differences, state))
	comparison.Differences = counted.Differences
	comparison.Meaningful = counted.Meaningful
	comparison.SystemManaged = counted.SystemManaged
	comparison.Explained = counted.Explained
	comparison.NeedsAttention = counted.NeedsAttention
	comparison.Redacted = counted.Redacted
	if comparison.State == domain.ComparisonDiffers {
		comparison.Reason = differReason(counted)
	}
	return comparison
}

func explained(difference domain.StateDifference) bool {
	if difference.Explanation == nil {
		return false
	}
	switch difference.Explanation.Cause {
	case domain.CauseController, domain.CauseArgoIgnored:
		return true
	default:
		return false
	}
}

func differReason(out domain.StateComparison) string {
	unexplained := out.Meaningful - out.Explained
	switch {
	case unexplained > 0 && out.Explained > 0:
		return fmt.Sprintf("%d unexplained %s, %d accounted for by a controller or an ignore rule.",
			unexplained, plural(unexplained, "difference", "differences"), out.Explained)
	case unexplained > 0:
		return fmt.Sprintf("%d %s between the source manifest and the object in the cluster.",
			out.Meaningful, plural(out.Meaningful, "difference", "differences"))
	case out.Explained > 0:
		if controllerManaged(out) {
			return fmt.Sprintf("%d controller-managed %s.",
				out.Explained, plural(out.Explained, "difference", "differences"))
		}
		return fmt.Sprintf("%d expected managed %s.",
			out.Explained, plural(out.Explained, "difference", "differences"))
	default:
		return fmt.Sprintf("%d %s between the source manifest and the object in the cluster.",
			out.Meaningful, plural(out.Meaningful, "difference", "differences"))
	}
}

// analyse enriches differences with what the cluster can prove about them.
//
// It never removes a difference and never changes its class. Classification
// already decided whether the row belongs in front of the reader; this only
// says why, when it can.
func analyse(differences []domain.StateDifference, state analysis) []domain.StateDifference {
	if state.live == nil {
		return differences
	}

	targets := matchingHPAs(state.hpas, state.live)
	rules := ignoreRules(state.app)

	for index := range differences {
		difference := &differences[index]
		if difference.Path != "spec.replicas" || state.live.GetKind() != "Deployment" {
			continue
		}
		difference.Explanation = explainReplicas(*difference, state, targets, rules)
	}
	return differences
}

func controllerManaged(out domain.StateComparison) bool {
	for _, difference := range out.Differences {
		if difference.Explanation != nil && difference.Explanation.Cause == domain.CauseController {
			return true
		}
	}
	return false
}

func explainReplicas(
	difference domain.StateDifference,
	state analysis,
	targets []*unstructured.Unstructured,
	rules []ignoreRule,
) *domain.DifferenceExplanation {
	owners := fieldOwners(state.live, difference.Path)
	named := managersOf(owners)

	var evidence []domain.DifferenceEvidence
	var unchecked []string

	for _, hpa := range targets {
		evidence = append(evidence, hpaEvidence(hpa, state.live))
	}
	if state.hpaErr != nil {
		unchecked = append(unchecked, "HorizontalPodAutoscalers in this namespace could not be listed.")
	}

	ignored := false
	if state.argoErr != nil {
		unchecked = append(unchecked, "The Application's ignoreDifferences could not be read.")
	} else {
		for _, rule := range rules {
			if !rule.appliesTo(state.live) {
				continue
			}
			covers, skipped := rule.covers(difference.Path, named)
			unchecked = append(unchecked, skipped...)
			if covers {
				ignored = true
				evidence = append(evidence, ignoreEvidence(rule, difference.Path))
			}
		}
	}

	evidence = append(evidence, owners...)

	if len(targets) == 0 && !ignored && len(unchecked) == 0 && len(owners) == 0 {
		return nil
	}
	if len(targets) == 0 && !ignored && len(unchecked) == 0 {
		// A manager name on the field is supporting evidence, not a cause.
		// Showing it without a controller or an ignore rule would be turning
		// "kubectl" into "a user scaled this", which the metadata does not
		// actually say.
		return &domain.DifferenceExplanation{
			Cause:      domain.CauseUnknown,
			Confidence: domain.ConfidenceSupporting,
			Summary:    "No controller ownership was identified.",
			Evidence:   owners,
		}
	}

	out := &domain.DifferenceExplanation{
		Evidence:          evidence,
		Unchecked:         unchecked,
		ApplicationIgnore: ignoreStatus(state, ignored),
	}

	switch {
	case len(targets) > 0:
		out.Cause = domain.CauseController
		out.Confidence = domain.ConfidenceConfirmed
		out.Attention = domain.AttentionNone
		out.ManagedBy = "HorizontalPodAutoscaler / " + targets[0].GetName()
		out.Summary = replicaSummary(targets[0], difference.Live)
		out.Note = argoNote(out.ApplicationIgnore)
	case ignored:
		out.Cause = domain.CauseArgoIgnored
		out.Confidence = domain.ConfidenceUnknown
		out.Attention = domain.AttentionReview
		out.Summary = "Argo CD ignores /spec/replicas, but no controller was found that would be writing the live value."
	default:
		out.Cause = domain.CauseUnknown
		out.Confidence = domain.ConfidenceUnknown
		out.Attention = domain.AttentionReview
		out.Summary = "Why this field differs could not be fully checked."
	}
	return out
}

func ignoreStatus(state analysis, ignored bool) string {
	switch {
	case state.argoErr != nil:
		return "unread"
	case state.app == nil:
		return ""
	case ignored:
		return "applies"
	default:
		return "absent"
	}
}

func replicaSummary(hpa *unstructured.Unstructured, live string) string {
	desired, ok := nestedNumber(hpa.Object, "status", "desiredReplicas")
	if ok && desired == live {
		return "HPA explains the live replica count."
	}
	return "HPA is managing replicas."
}

func argoNote(status string) string {
	switch status {
	case "applies":
		return "The Application ignores /spec/replicas, so Synced is expected."
	case "absent":
		return "No Application-level ignore rule for /spec/replicas was found. Argo CD may still be normalising or ignoring this field elsewhere."
	case "unread":
		return "Whether the Application ignores /spec/replicas could not be checked."
	default:
		return ""
	}
}

// markImplicit fills in a source value the manifest omitted, when Kubernetes
// has a documented default for the field.
//
// It is not normalisation: the two values still differ, and the row stays.
// It is also not explanation: "the source is silent and Kubernetes would
// have used 1" is a fact about the file, not about who wrote the live value.
func markImplicit(differences []domain.StateDifference, source map[string]any, kind string) []domain.StateDifference {
	if kind != "Deployment" {
		return differences
	}
	for index := range differences {
		difference := &differences[index]
		if difference.Path != "spec.replicas" {
			continue
		}
		if sets(source, "spec", "replicas") {
			continue
		}
		if difference.Source != "" {
			continue
		}
		difference.Source = "1"
		difference.SourceImplicit = true
	}
	return differences
}

func nestedNumber(object map[string]any, path ...string) (string, bool) {
	value, found, err := unstructured.NestedFieldNoCopy(object, path...)
	if !found || err != nil || value == nil {
		return "", false
	}
	switch typed := value.(type) {
	case int64:
		return strconv.FormatInt(typed, 10), true
	case int32:
		return strconv.FormatInt(int64(typed), 10), true
	case int:
		return strconv.Itoa(typed), true
	case float64:
		return strconv.FormatInt(int64(typed), 10), true
	case json.Number:
		return typed.String(), true
	default:
		written := fmt.Sprint(typed)
		if written == "" {
			return "", false
		}
		return written, true
	}
}
