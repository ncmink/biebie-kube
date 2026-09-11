package query

import (
	"strings"

	"biebie-kube/internal/domain"
)

// MatchResult is the outcome of evaluating one row.
type MatchResult int

const (
	MatchFalse MatchResult = iota
	MatchTrue
	MatchUnknown
)

// Match reports whether a row satisfies the compiled program.
func (prog Program) Match(row domain.ResourceRow, ctx EvalContext) MatchResult {
	switch prog.mode {
	case domain.QueryModeText, "":
		if prog.textNeedle == "" {
			return MatchTrue
		}
		if strings.Contains(strings.ToLower(row.Name), prog.textNeedle) {
			return MatchTrue
		}
		return MatchFalse

	case domain.QueryModeExpression:
		if len(prog.terms) == 0 {
			return MatchTrue
		}
		proj := Project(row, ctx)
		for _, term := range prog.terms {
			switch evalTerm(term, proj) {
			case MatchFalse:
				return MatchFalse
			case MatchUnknown:
				return MatchUnknown
			}
		}
		return MatchTrue
	default:
		return MatchFalse
	}
}

func evalTerm(term term, proj Projection) MatchResult {
	switch term.kind {
	case termMissing:
		if fieldUnknown(term.field, proj) {
			return MatchTrue
		}
		return MatchFalse
	case termCompare:
		if isStringField(term.field) {
			left := stringField(term.field, proj)
			switch term.op {
			case opEq:
				if left == term.text {
					return MatchTrue
				}
				return MatchFalse
			case opNe:
				if left != term.text {
					return MatchTrue
				}
				return MatchFalse
			}
			return MatchFalse
		}

		value, ok := numericField(term.field, proj)
		if !ok {
			return MatchUnknown
		}
		switch term.op {
		case opEq:
			if value == term.number {
				return MatchTrue
			}
			return MatchFalse
		case opNe:
			if value != term.number {
				return MatchTrue
			}
			return MatchFalse
		case opGt:
			if value > term.number {
				return MatchTrue
			}
			return MatchFalse
		case opGte:
			if value >= term.number {
				return MatchTrue
			}
			return MatchFalse
		case opLt:
			if value < term.number {
				return MatchTrue
			}
			return MatchFalse
		case opLte:
			if value <= term.number {
				return MatchTrue
			}
			return MatchFalse
		}
	}
	return MatchFalse
}

func stringField(f field, proj Projection) string {
	switch f {
	case fieldName:
		return proj.Name
	case fieldNamespace:
		return proj.Namespace
	case fieldStatus:
		return proj.Status
	case fieldHealth:
		return proj.Health
	default:
		return ""
	}
}

func numericField(f field, proj Projection) (float64, bool) {
	var value NumericValue
	switch f {
	case fieldRestarts:
		value = proj.Restarts
	case fieldAge:
		value = proj.Age
	case fieldCPU:
		value = proj.CPU
	case fieldMemory:
		value = proj.Memory
	default:
		return 0, false
	}
	if value.Kind != ValuePresent {
		return 0, false
	}
	return value.Number, true
}

func fieldUnknown(f field, proj Projection) bool {
	if isStringField(f) {
		return false
	}
	_, ok := numericField(f, proj)
	return !ok
}

// FilterRows applies a program to every row and returns matched rows plus unknown count.
func FilterRows(rows []domain.ResourceRow, prog Program, ctx EvalContext) (matched []domain.ResourceRow, unknown int) {
	for _, row := range rows {
		switch prog.Match(row, ctx) {
		case MatchTrue:
			matched = append(matched, row)
		case MatchUnknown:
			unknown++
		}
	}
	return matched, unknown
}
