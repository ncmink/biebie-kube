package gitops

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// An ignore rule is one spec.ignoreDifferences entry, evaluated against one
// object and one field.
//
// Selectors that are present must match. Selectors that are absent apply to
// every object of that kind, which is how a rule written as `kind: Deployment`
// with no name covers every Deployment the Application manages.
type ignoreRule struct {
	group     string
	kind      string
	name      string
	namespace string
	pointers  []string
	jq        []string
	managers  []string
}

func ignoreRules(app *unstructured.Unstructured) []ignoreRule {
	if app == nil {
		return nil
	}
	list, _, _ := unstructured.NestedSlice(app.Object, "spec", "ignoreDifferences")
	out := make([]ignoreRule, 0, len(list))
	for _, item := range list {
		object, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rule := ignoreRule{
			group:     stringField(object, "group"),
			kind:      stringField(object, "kind"),
			name:      stringField(object, "name"),
			namespace: stringField(object, "namespace"),
			pointers:  stringList(object, "jsonPointers"),
			jq:        stringList(object, "jqPathExpressions"),
			managers:  stringList(object, "managedFieldsManagers"),
		}
		out = append(out, rule)
	}
	return out
}

func (r ignoreRule) appliesTo(live *unstructured.Unstructured) bool {
	if r.kind != "" && r.kind != live.GetKind() {
		return false
	}
	if r.name != "" && r.name != live.GetName() {
		return false
	}
	if r.namespace != "" && r.namespace != live.GetNamespace() {
		return false
	}
	if r.group != "" && r.group != apiGroup(live.GetAPIVersion()) {
		return false
	}
	return true
}

// covers reports whether the rule ignores this field path, and which of its
// forms were not evaluated.
//
// jsonPointers are matched exactly. jqPathExpressions are matched only in the
// handful of forms that name a field without computation — evaluating jq
// would be recreating Argo CD's diff engine, which this slice is not.
// managedFieldsManagers cover the field only when that manager actually owns
// it on the live object.
func (r ignoreRule) covers(path string, owners []string) (applies bool, unevaluated []string) {
	pointer := jsonPointer(path)
	for _, listed := range r.pointers {
		if listed == pointer {
			applies = true
		}
	}
	for _, expr := range r.jq {
		switch {
		case simpleJQ(expr) == path:
			applies = true
		default:
			unevaluated = append(unevaluated, "jqPathExpression "+expr)
		}
	}
	if len(r.managers) > 0 {
		for _, manager := range r.managers {
			for _, owner := range owners {
				if manager == owner {
					applies = true
				}
			}
		}
	}
	return applies, unevaluated
}

func jsonPointer(path string) string {
	if path == "" {
		return ""
	}
	return "/" + strings.ReplaceAll(path, ".", "/")
}

// simpleJQ maps a jq expression that is just a field path onto the dotted
// path this comparison uses. Anything with filters, pipes or iterators is
// left unevaluated.
func simpleJQ(expr string) string {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimPrefix(expr, ".")
	if expr == "" || strings.ContainsAny(expr, "|[]()?") {
		return ""
	}
	return expr
}

func ignoreEvidence(rule ignoreRule, path string) domain.DifferenceEvidence {
	pointer := jsonPointer(path)
	out := domain.DifferenceEvidence{
		Kind:       domain.EvidenceArgoIgnore,
		Confidence: domain.ConfidenceConfirmed,
		Subject:    pointer,
		Summary:    "Argo CD ignores " + pointer + ".",
		Facts:      []domain.EvidenceFact{{Name: "Pointer", Value: pointer}},
	}
	if rule.group != "" {
		out.Facts = append(out.Facts, domain.EvidenceFact{Name: "Group", Value: rule.group})
	}
	if rule.kind != "" {
		out.Facts = append(out.Facts, domain.EvidenceFact{Name: "Kind", Value: rule.kind})
	}
	if rule.name != "" {
		out.Facts = append(out.Facts, domain.EvidenceFact{Name: "Name", Value: rule.name})
	}
	return out
}

func stringField(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

func stringList(object map[string]any, key string) []string {
	raw, ok := object[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if value, ok := item.(string); ok && value != "" {
			out = append(out, value)
		}
	}
	return out
}
