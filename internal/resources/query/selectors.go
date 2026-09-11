package query

import (
	"strings"

	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"

	"biebie-kube/internal/domain"
)

// Selectors holds canonical server-scope selectors for one table query.
type Selectors struct {
	Label string
	Field string

	label labels.Selector
	field fields.Selector
}

// Empty reports whether server scope is unfiltered.
func (s Selectors) Empty() bool {
	return s.Label == "" && s.Field == ""
}

// ParseSelectors validates and canonicalises label and field selector strings.
func ParseSelectors(label, field string) (Selectors, domain.QueryDiagnostic) {
	out := Selectors{}
	label = strings.TrimSpace(label)
	field = strings.TrimSpace(field)

	if label != "" {
		parsed, err := labels.Parse(label)
		if err != nil {
			return out, domain.QueryDiagnostic{
				Valid: false,
				Error: "This label filter is not valid. Check the key and value, then try again.",
			}
		}
		out.label = parsed
		out.Label = parsed.String()
	}

	if field != "" {
		parsed, err := fields.ParseSelector(field)
		if err != nil {
			return out, domain.QueryDiagnostic{
				Valid: false,
				Error: "This resource property filter is not valid. Advanced filters use field=value, for example status.phase=Running.",
			}
		}
		out.field = parsed
		out.Field = parsed.String()
	}

	return out, domain.QueryDiagnostic{Valid: true}
}

// ValidateQuery checks selectors and the filter expression together.
func ValidateQuery(q domain.ListQuery) domain.QueryDiagnostic {
	if _, diag := ParseSelectors(q.LabelSelector, q.FieldSelector); !diag.Valid {
		return diag
	}
	_, diag := Compile(q)
	return diag
}

// MatchesLabels reports whether object labels satisfy the label selector.
func (s Selectors) MatchesLabels(set labels.Set) bool {
	if s.label == nil {
		return true
	}
	return s.label.Matches(set)
}
