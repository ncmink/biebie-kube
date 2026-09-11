package query

import (
	"testing"
	"time"

	"biebie-kube/internal/domain"
)

func TestCompileExpressionExamples(t *testing.T) {
	cases := []string{
		`restarts >= 5 && age < 2h`,
		`cpu > 500m`,
		`memory >= 1Gi`,
		`status = CrashLoopBackOff`,
		`health != healthy`,
		`missing(cpu)`,
	}
	for _, expr := range cases {
		_, diag := Compile(domain.ListQuery{Mode: domain.QueryModeExpression, Expression: expr})
		if !diag.Valid {
			t.Fatalf("%q: %s", expr, diag.Error)
		}
	}
}

func TestCompileRejectsUnknownField(t *testing.T) {
	_, diag := Compile(domain.ListQuery{Mode: domain.QueryModeExpression, Expression: `pods > 1`})
	if diag.Valid {
		t.Fatal("expected invalid")
	}
}

func TestCompileRejectsAgeWithoutUnit(t *testing.T) {
	_, diag := Compile(domain.ListQuery{Mode: domain.QueryModeExpression, Expression: `age < 2`})
	if diag.Valid {
		t.Fatal("expected invalid")
	}
}

func TestCPUQuantityEquivalence(t *testing.T) {
	now := time.Now().UTC()
	fetched := now.Add(-10 * time.Second)
	ctx := EvalContext{Now: now, MetricsFetchedAt: &fetched}

	row := domain.ResourceRow{
		Name: "demo",
		Fields: map[string]string{"cpu": "500m"},
	}
	prog, diag := Compile(domain.ListQuery{Mode: domain.QueryModeExpression, Expression: `cpu = 0.5`})
	if !diag.Valid {
		t.Fatal(diag.Error)
	}
	if prog.Match(row, ctx) != MatchTrue {
		t.Fatal("0.5 cores should equal 500m")
	}
}

func TestMissingCPUMatchesUnknownUsage(t *testing.T) {
	ctx := EvalContext{Now: time.Now().UTC()}
	row := domain.ResourceRow{Name: "demo", Fields: map[string]string{}}

	prog, _ := Compile(domain.ListQuery{Mode: domain.QueryModeExpression, Expression: `missing(cpu)`})
	if prog.Match(row, ctx) != MatchTrue {
		t.Fatal("missing cpu should match row without usage")
	}

	prog, _ = Compile(domain.ListQuery{Mode: domain.QueryModeExpression, Expression: `cpu > 500m`})
	if prog.Match(row, ctx) != MatchUnknown {
		t.Fatal("cpu comparison with unknown should be unknown")
	}
}

func TestStaleMetricsAreUnknown(t *testing.T) {
	now := time.Now().UTC()
	stale := now.Add(-60 * time.Second)
	ctx := EvalContext{Now: now, MetricsFetchedAt: &stale}
	row := domain.ResourceRow{
		Name:   "demo",
		Fields: map[string]string{"cpu": "500m"},
	}

	prog, _ := Compile(domain.ListQuery{Mode: domain.QueryModeExpression, Expression: `cpu > 100m`})
	if prog.Match(row, ctx) != MatchUnknown {
		t.Fatal("stale metrics should evaluate as unknown")
	}
}

func TestTextModeStillMatchesNameSubstring(t *testing.T) {
	prog, diag := Compile(domain.ListQuery{Filter: "API"})
	if !diag.Valid {
		t.Fatal(diag.Error)
	}
	if prog.Match(domain.ResourceRow{Name: "shop-api"}, EvalContext{}) != MatchTrue {
		t.Fatal("text mode should stay case-insensitive on name")
	}
}
