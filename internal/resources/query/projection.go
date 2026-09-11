package query

import (
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"

	"biebie-kube/internal/domain"
)

// ValueKind says whether a typed field is present for evaluation.
type ValueKind int

const (
	ValuePresent ValueKind = iota
	ValueUnknown
)

// NumericValue is one comparable field on a row.
type NumericValue struct {
	Kind ValueKind
	// Number holds millicores, bytes, restart count, or age in seconds.
	Number float64
}

// Projection is the typed view of one row for expression evaluation.
type Projection struct {
	Name      string
	Namespace string
	Status    string
	Health    string
	Restarts  NumericValue
	Age       NumericValue
	CPU       NumericValue
	Memory    NumericValue
}

// EvalContext carries timing shared across one filter pass.
type EvalContext struct {
	Now              time.Time
	MetricsFetchedAt *time.Time
}

// Project builds typed values from a rendered row.
func Project(row domain.ResourceRow, ctx EvalContext) Projection {
	now := ctx.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}

	p := Projection{
		Name:      row.Name,
		Namespace: row.Namespace,
		Status:    row.Status,
		Health:    string(row.Health),
		Restarts:  NumericValue{Kind: ValueUnknown},
		Age:       NumericValue{Kind: ValueUnknown},
		CPU:       NumericValue{Kind: ValueUnknown},
		Memory:    NumericValue{Kind: ValueUnknown},
	}

	if raw := strings.TrimSpace(row.Fields["restarts"]); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n >= 0 {
			p.Restarts = NumericValue{Kind: ValuePresent, Number: float64(n)}
		}
	}

	if !row.CreatedAt.IsZero() {
		age := now.Sub(row.CreatedAt)
		if age < 0 {
			age = 0
		}
		p.Age = NumericValue{Kind: ValuePresent, Number: age.Seconds()}
	}

	metricsFresh := metricsFresh(ctx.MetricsFetchedAt, now)
	if cpu, ok := parseCPU(row.Fields["cpu"]); ok && metricsFresh {
		p.CPU = NumericValue{Kind: ValuePresent, Number: float64(cpu)}
	}
	if memory, ok := parseMemory(row.Fields["memory"]); ok && metricsFresh {
		p.Memory = NumericValue{Kind: ValuePresent, Number: float64(memory)}
	}

	return p
}

func metricsFresh(fetchedAt *time.Time, now time.Time) bool {
	if fetchedAt == nil || fetchedAt.IsZero() {
		return false
	}
	return now.Sub(*fetchedAt) <= metricsStaleAfter*time.Second
}

func parseCPU(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	q, err := resource.ParseQuantity(raw)
	if err != nil {
		// Bare number means cores.
		if f, err := strconv.ParseFloat(raw, 64); err == nil && f >= 0 {
			return int64(f * 1000), true
		}
		return 0, false
	}
	return q.MilliValue(), true
}

func parseMemory(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	q, err := resource.ParseQuantity(raw)
	if err != nil {
		if f, err := strconv.ParseFloat(raw, 64); err == nil && f >= 0 {
			return int64(f), true
		}
		return 0, false
	}
	return q.Value(), true
}
