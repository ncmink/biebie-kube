package views

import (
	"fmt"
	"slices"
	"strings"

	"biebie-kube/internal/domain"
)

// Resolve checks whether a saved view can still be applied in the current cluster session.
func Resolve(
	view domain.SavedView,
	catalogue []domain.KindInfo,
	namespaces []string,
	parseQuery func(domain.ListQuery) domain.QueryDiagnostic,
) domain.SavedViewResolution {
	out := domain.SavedViewResolution{View: view}

	if view.QueryVersion != domain.SavedViewQueryVersion {
		out.Issues = append(out.Issues, domain.SavedViewIssueDetail{
			Code:    domain.SavedViewIssueUnknownFormat,
			Message: "This saved view uses an unknown query format and cannot be applied.",
		})
		return out
	}

	info, kindOK := findKind(catalogue, view.Kind)
	if !kindOK {
		out.Issues = append(out.Issues, domain.SavedViewIssueDetail{
			Code:    domain.SavedViewIssueKindMissing,
			Message: fmt.Sprintf("Resource type %q is not available in this cluster.", view.Kind),
		})
	}

	if view.Namespace != "" {
		if !slices.Contains(namespaces, view.Namespace) {
			out.Issues = append(out.Issues, domain.SavedViewIssueDetail{
				Code:    domain.SavedViewIssueNamespaceMissing,
				Message: fmt.Sprintf("Namespace %q is not available.", view.Namespace),
			})
		}
	}

	if kindOK && len(view.ColumnIDs) > 0 {
		allowed := columnKeys(info)
		for _, key := range view.ColumnIDs {
			if !slices.Contains(allowed, key) {
				out.Issues = append(out.Issues, domain.SavedViewIssueDetail{
					Code:    domain.SavedViewIssueColumnMissing,
					Message: fmt.Sprintf("Column %q is not available for this resource type.", key),
				})
			}
		}
	}

	query := domain.ListQuery{
		Namespace:     view.Namespace,
		Mode:          view.Mode,
		Filter:        view.Filter,
		Expression:    view.Expression,
		LabelSelector: view.LabelSelector,
		FieldSelector: view.FieldSelector,
		SortKey:       view.SortKey,
		SortDesc:      view.SortDesc,
	}
	if diag := parseQuery(query); !diag.Valid {
		out.Issues = append(out.Issues, domain.SavedViewIssueDetail{
			Code:    domain.SavedViewIssueQueryInvalid,
			Message: friendlyQueryError(diag.Error),
		})
	}

	out.Valid = len(out.Issues) == 0
	return out
}

func findKind(catalogue []domain.KindInfo, kind domain.Kind) (domain.KindInfo, bool) {
	for _, info := range catalogue {
		if info.Kind == kind {
			return info, true
		}
	}
	return domain.KindInfo{}, false
}

func columnKeys(info domain.KindInfo) []string {
	keys := make([]string, 0, len(info.Columns))
	for _, column := range info.Columns {
		keys = append(keys, column.Key)
	}
	return keys
}

func friendlyQueryError(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "The saved filter is no longer valid."
	}
	if strings.Contains(raw, "field selector is not supported") {
		return "This resource type does not support one of the saved property filters. Edit the view or use labels instead."
	}
	return raw
}
