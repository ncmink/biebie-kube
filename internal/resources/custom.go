package resources

import (
	"fmt"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/util/jsonpath"

	"biebie-kube/internal/domain"
)

// renderCustom fills a row for a resource this application has no knowledge of.
//
// Everything shown comes from the cluster itself: the columns are the ones the
// definition declares, and the health is read from the status conventions
// Kubernetes controllers actually follow. Nothing is inferred from the kind's
// name, because an operator's resource means whatever its author decided.
func renderCustom(
	info domain.KindInfo,
	obj *unstructured.Unstructured,
) (domain.Health, string, map[string]string) {
	fields := make(map[string]string, len(info.Columns))
	for _, column := range info.Columns {
		if column.Path == "" {
			continue
		}
		fields[column.Key] = evaluate(column.Path, obj)
	}

	health, status := customHealth(obj)
	if health == domain.HealthUnknown {
		// A definition that declares a status column has already said where its
		// verdict lives, and many controllers report there and nowhere else —
		// an Argo CD Application carries no conditions at all, only the health
		// its own column points at.
		if derived, text := healthFromColumns(info.Columns, fields); derived != domain.HealthUnknown {
			health, status = derived, text
		}
	}
	return health, status, fields
}

// healthFromColumns reads a verdict out of a column the definition declared.
//
// A column naming health is preferred over one naming status: a resource can be
// synced and unhealthy at once, and the unhealthy half is what needs attention.
func healthFromColumns(columns []domain.Column, fields map[string]string) (domain.Health, string) {
	var fallback string
	for _, column := range columns {
		title := strings.ToLower(column.Title)
		value := fields[column.Key]
		if value == "" {
			continue
		}
		if strings.Contains(title, "health") {
			return phaseHealth(value), value
		}
		if fallback == "" && strings.Contains(title, "status") {
			fallback = value
		}
	}
	if fallback == "" {
		return domain.HealthUnknown, ""
	}
	return phaseHealth(fallback), fallback
}

// parsers caches compiled JSONPath expressions.
//
// A table of two thousand rows evaluates every column once per row. Compiling
// the same handful of expressions that many times is pure waste, and the set of
// expressions is bounded by the definitions installed in the cluster.
var parsers sync.Map

func parserFor(path string) (*jsonpath.JSONPath, error) {
	if cached, ok := parsers.Load(path); ok {
		switch typed := cached.(type) {
		case *jsonpath.JSONPath:
			return typed, nil
		case error:
			return nil, typed
		}
	}

	// A definition writes its paths the way kubectl accepts them on the command
	// line, without the surrounding braces the parser expects.
	parser := jsonpath.New("column").AllowMissingKeys(true)
	if err := parser.Parse(fmt.Sprintf("{%s}", path)); err != nil {
		parsers.Store(path, err)
		return nil, err
	}
	parsers.Store(path, parser)
	return parser, nil
}

// evaluate reads one column's value out of an object.
//
// A path that does not match is an empty cell rather than an error: a resource
// whose controller has not filled in its status yet is the normal case for
// anything just created.
func evaluate(path string, obj *unstructured.Unstructured) string {
	parser, err := parserFor(path)
	if err != nil {
		return ""
	}

	results, err := parser.FindResults(obj.Object)
	if err != nil {
		return ""
	}

	var parts []string
	for _, group := range results {
		for _, value := range group {
			if !value.IsValid() || !value.CanInterface() {
				continue
			}
			if text := display(value.Interface()); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, ", ")
}

func display(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case float64:
		// Every number in an unstructured object is a float64 after JSON
		// decoding, and a replica count shown as "3.000000" would be absurd.
		if typed == float64(int64(typed)) {
			return fmt.Sprintf("%d", int64(typed))
		}
		return fmt.Sprintf("%g", typed)
	default:
		return fmt.Sprint(typed)
	}
}

// positiveConditions are the condition types whose True means "working", in the
// order of how directly they answer that question.
var positiveConditions = []string{"Ready", "Available", "Healthy", "Synced", "Succeeded", "Established"}

// negativeConditions are the condition types whose True means the opposite.
var negativeConditions = []string{"Degraded", "Failed", "Error"}

// customHealth reads a traffic light out of a resource whose shape is unknown.
//
// Two conventions are honoured, both from the API conventions controllers are
// written against: a conditions list, and a phase string. A resource following
// neither is reported as unknown, which is honest — inventing a green light for
// something this application cannot assess is worse than admitting it.
func customHealth(obj *unstructured.Unstructured) (domain.Health, string) {
	conditions := nestedSlice(obj, "status", "conditions")

	byType := make(map[string]map[string]any, len(conditions))
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _, _ := unstructured.NestedString(condition, "type")
		if name != "" {
			byType[name] = condition
		}
	}

	for _, name := range negativeConditions {
		condition, ok := byType[name]
		if !ok {
			continue
		}
		if status, _, _ := unstructured.NestedString(condition, "status"); status == "True" {
			return domain.HealthCritical, conditionSummary(name, condition)
		}
	}

	for _, name := range positiveConditions {
		condition, ok := byType[name]
		if !ok {
			continue
		}
		status, _, _ := unstructured.NestedString(condition, "status")
		switch status {
		case "True":
			return domain.HealthHealthy, name
		case "False":
			return domain.HealthCritical, conditionSummary(name, condition)
		default:
			return domain.HealthProgress, conditionSummary(name, condition)
		}
	}

	if phase := nestedString(obj, "status", "phase"); phase != "" {
		return phaseHealth(phase), phase
	}
	return domain.HealthUnknown, ""
}

// conditionSummary names why a condition is not satisfied, preferring the
// controller's own reason over the condition type.
func conditionSummary(name string, condition map[string]any) string {
	if reason, _, _ := unstructured.NestedString(condition, "reason"); reason != "" {
		return reason
	}
	return "Not " + name
}

// phaseHealth maps the words controllers use to a traffic light.
//
// The vocabulary is deliberately closed. A word nobody agreed on gets no colour
// rather than a guess: an operator that reports "Reticulating" is telling the
// engineer something this application has no business interpreting.
func phaseHealth(phase string) domain.Health {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "running", "active", "succeeded", "success", "ready", "healthy", "bound",
		"available", "synced", "established", "complete", "completed", "true":
		return domain.HealthHealthy
	case "failed", "failure", "error", "degraded", "lost", "unhealthy", "missing", "false":
		return domain.HealthCritical
	case "outofsync", "out of sync", "suspended", "warning", "paused":
		return domain.HealthWarning
	case "pending", "progressing", "provisioning", "creating", "updating",
		"reconciling", "syncing", "terminating":
		return domain.HealthProgress
	default:
		return domain.HealthUnknown
	}
}

// renderCRD fills a row on the definitions page.
//
// A definition is worth listing by what it introduces — which group, which
// kind, and whether its objects live in a namespace — because that is what
// decides where an engineer looks for the objects themselves.
func renderCRD(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	fields := map[string]string{
		"group": nestedString(obj, "spec", "group"),
		"kind":  nestedString(obj, "spec", "names", "kind"),
		"scope": nestedString(obj, "spec", "scope"),
	}

	var served []string
	for _, raw := range nestedSlice(obj, "spec", "versions") {
		version, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if isServed, _, _ := unstructured.NestedBool(version, "served"); !isServed {
			continue
		}
		name, _, _ := unstructured.NestedString(version, "name")
		if name == "" {
			continue
		}
		if storage, _, _ := unstructured.NestedBool(version, "storage"); storage {
			// The stored version is the one this application reads, so it is
			// marked rather than left for the engineer to work out.
			name += "*"
		}
		served = append(served, name)
	}
	fields["versions"] = strings.Join(served, " ")

	health, status := customHealth(obj)
	return health, status, fields
}
