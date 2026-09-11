package query

import (
	"testing"

	"biebie-kube/internal/domain"
)

func TestParseSelectorsCanonicalisesLabel(t *testing.T) {
	sel, diag := ParseSelectors("app=shop", "")
	if !diag.Valid {
		t.Fatal(diag.Error)
	}
	if sel.Label != "app=shop" {
		t.Fatalf("label = %q", sel.Label)
	}
}

func TestValidateQueryRejectsBadFieldSelector(t *testing.T) {
	diag := ValidateQuery(domain.ListQuery{
		Mode:          domain.QueryModeExpression,
		Expression:    "restarts >= 1",
		FieldSelector: "not a selector",
	})
	if diag.Valid {
		t.Fatal("expected invalid field selector")
	}
}
