package query

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"biebie-kube/internal/domain"
)

// Program is a compiled expression ready to evaluate.
type Program struct {
	mode       domain.QueryMode
	textNeedle string
	terms      []term
}

type term struct {
	kind termKind

	field field
	op    op

	// string value for equality on string fields
	text string
	// numeric value for ordering comparisons
	number float64
}

type termKind int

const (
	termCompare termKind = iota
	termMissing
)

type field int

const (
	fieldName field = iota
	fieldNamespace
	fieldStatus
	fieldHealth
	fieldRestarts
	fieldAge
	fieldCPU
	fieldMemory
)

type op int

const (
	opEq op = iota
	opNe
	opGt
	opGte
	opLt
	opLte
)

// Compile turns a list query into an executable program.
func Compile(q domain.ListQuery) (Program, domain.QueryDiagnostic) {
	mode := q.Mode
	if mode == "" {
		mode = domain.QueryModeText
	}

	switch mode {
	case domain.QueryModeText:
		return Program{
			mode:       domain.QueryModeText,
			textNeedle: strings.ToLower(strings.TrimSpace(q.Filter)),
		}, domain.QueryDiagnostic{Valid: true}

	case domain.QueryModeExpression:
		expr := strings.TrimSpace(q.Expression)
		if expr == "" {
			return Program{mode: domain.QueryModeExpression}, domain.QueryDiagnostic{Valid: true}
		}
		if len(expr) > maxInputBytes {
			return Program{}, domain.QueryDiagnostic{
				Valid: false,
				Error: fmt.Sprintf("expression exceeds %d bytes", maxInputBytes),
			}
		}
		terms, diag := parseExpression(expr)
		if !diag.Valid {
			return Program{}, diag
		}
		return Program{mode: domain.QueryModeExpression, terms: terms}, domain.QueryDiagnostic{Valid: true}

	default:
		return Program{}, domain.QueryDiagnostic{
			Valid: false,
			Error: fmt.Sprintf("unknown query mode %q", mode),
		}
	}
}

func parseExpression(input string) ([]term, domain.QueryDiagnostic) {
	p := &parser{input: input}
	terms, err := p.parseTerms()
	if err != nil {
		return nil, domain.QueryDiagnostic{
			Valid:    false,
			Error:    err.Error(),
			Position: errPos(err),
		}
	}
	if len(terms) > maxTerms {
		return nil, domain.QueryDiagnostic{
			Valid: false,
			Error: fmt.Sprintf("expression exceeds %d terms", maxTerms),
		}
	}
	return terms, domain.QueryDiagnostic{Valid: true}
}

type parseError struct {
	msg string
	at  int
}

func (e *parseError) Error() string { return e.msg }

func errPos(err error) int {
	if pe, ok := err.(*parseError); ok {
		return pe.at
	}
	return 0
}

type parser struct {
	input string
	at    int
}

func (p *parser) parseTerms() ([]term, error) {
	first, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	out := []term{first}
	for {
		p.skipSpace()
		if p.at >= len(p.input) {
			return out, nil
		}
		if !p.matchKeyword("AND") && !p.matchLiteral("&&") {
			return nil, &parseError{msg: "expected AND or &&", at: p.at}
		}
		next, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		out = append(out, next)
	}
}

func (p *parser) parseTerm() (term, error) {
	p.skipSpace()
	if p.matchKeyword("missing") {
		p.skipSpace()
		if !p.matchLiteral("(") {
			return term{}, &parseError{msg: "expected ( after missing", at: p.at}
		}
		p.skipSpace()
		field, err := p.readField()
		if err != nil {
			return term{}, err
		}
		p.skipSpace()
		if !p.matchLiteral(")") {
			return term{}, &parseError{msg: "expected )", at: p.at}
		}
		return term{kind: termMissing, field: field}, nil
	}

	field, err := p.readField()
	if err != nil {
		return term{}, err
	}
	p.skipSpace()
	op, err := p.readOp()
	if err != nil {
		return term{}, err
	}
	p.skipSpace()
	if isStringField(field) {
		if op != opEq && op != opNe {
			return term{}, &parseError{
				msg: fmt.Sprintf("%s supports only = and !=", fieldLabel(field)),
				at:  p.at,
			}
		}
		text, err := p.readStringValue()
		if err != nil {
			return term{}, err
		}
		return term{kind: termCompare, field: field, op: op, text: text}, nil
	}

	number, err := p.readNumericValue(field)
	if err != nil {
		return term{}, err
	}
	if (op == opEq || op == opNe) && field == fieldRestarts && number != float64(int64(number)) {
		return term{}, &parseError{msg: "restarts must be an integer", at: p.at}
	}
	return term{kind: termCompare, field: field, op: op, number: number}, nil
}

func (p *parser) readField() (field, error) {
	start := p.at
	for p.at < len(p.input) {
		r, size := utf8.DecodeRuneInString(p.input[p.at:])
		if !unicode.IsLetter(r) {
			break
		}
		p.at += size
	}
	if start == p.at {
		return 0, &parseError{msg: "expected field name", at: p.at}
	}
	name := strings.ToLower(p.input[start:p.at])
	switch name {
	case "name":
		return fieldName, nil
	case "namespace":
		return fieldNamespace, nil
	case "status":
		return fieldStatus, nil
	case "health":
		return fieldHealth, nil
	case "restarts":
		return fieldRestarts, nil
	case "age":
		return fieldAge, nil
	case "cpu":
		return fieldCPU, nil
	case "memory":
		return fieldMemory, nil
	default:
		return 0, &parseError{msg: fmt.Sprintf("unknown field %q", name), at: start}
	}
}

func (p *parser) readOp() (op, error) {
	switch {
	case p.matchLiteral(">="):
		return opGte, nil
	case p.matchLiteral("<="):
		return opLte, nil
	case p.matchLiteral("!="):
		return opNe, nil
	case p.matchLiteral("="):
		return opEq, nil
	case p.matchLiteral(">"):
		return opGt, nil
	case p.matchLiteral("<"):
		return opLt, nil
	default:
		return 0, &parseError{msg: "expected comparison operator", at: p.at}
	}
}

func (p *parser) readStringValue() (string, error) {
	p.skipSpace()
	if p.at >= len(p.input) {
		return "", &parseError{msg: "expected string value", at: p.at}
	}
	switch p.input[p.at] {
	case '"', '\'':
		quote := p.input[p.at]
		p.at++
		start := p.at
		for p.at < len(p.input) && p.input[p.at] != quote {
			p.at++
		}
		if p.at >= len(p.input) {
			return "", &parseError{msg: "unterminated string", at: start}
		}
		value := p.input[start:p.at]
		p.at++
		return value, nil
	default:
		start := p.at
		for p.at < len(p.input) {
			r, size := utf8.DecodeRuneInString(p.input[p.at:])
			if unicode.IsSpace(r) || r == '&' {
				break
			}
			if strings.HasPrefix(p.input[p.at:], "&&") {
				break
			}
			if p.matchKeywordAt("AND") {
				break
			}
			p.at += size
		}
		if start == p.at {
			return "", &parseError{msg: "expected string value; quote values with spaces", at: p.at}
		}
		return p.input[start:p.at], nil
	}
}

func (p *parser) readNumericValue(field field) (float64, error) {
	start := p.at
	p.skipSpace()
	if p.at >= len(p.input) {
		return 0, &parseError{msg: "expected numeric value", at: p.at}
	}

	// Read token until space or AND.
	tokenStart := p.at
	for p.at < len(p.input) {
		if unicode.IsSpace(rune(p.input[p.at])) {
			break
		}
		if strings.HasPrefix(p.input[p.at:], "&&") {
			break
		}
		if p.matchKeywordAt("AND") {
			break
		}
		p.at++
	}
	token := strings.TrimSpace(p.input[tokenStart:p.at])
	if token == "" {
		return 0, &parseError{msg: "expected numeric value", at: start}
	}

	switch field {
	case fieldAge:
		d, err := parseDuration(token)
		if err != nil {
			return 0, &parseError{msg: err.Error(), at: tokenStart}
		}
		return d.Seconds(), nil
	case fieldCPU:
		milli, ok := parseCPU(token)
		if !ok {
			return 0, &parseError{msg: fmt.Sprintf("invalid cpu quantity %q", token), at: tokenStart}
		}
		return float64(milli), nil
	case fieldMemory:
		bytes, ok := parseMemory(token)
		if !ok {
			return 0, &parseError{msg: fmt.Sprintf("invalid memory quantity %q", token), at: tokenStart}
		}
		return float64(bytes), nil
	case fieldRestarts:
		if strings.ContainsAny(token, ".") {
			return 0, &parseError{msg: "restarts must be an integer", at: tokenStart}
		}
		n, err := parseInt(token)
		if err != nil || n < 0 {
			return 0, &parseError{msg: "restarts must be a non-negative integer", at: tokenStart}
		}
		return float64(n), nil
	default:
		return 0, &parseError{msg: "internal: numeric read on string field", at: tokenStart}
	}
}

func (p *parser) skipSpace() {
	for p.at < len(p.input) && unicode.IsSpace(rune(p.input[p.at])) {
		p.at++
	}
}

func (p *parser) matchLiteral(lit string) bool {
	if !strings.HasPrefix(p.input[p.at:], lit) {
		return false
	}
	p.at += len(lit)
	return true
}

func (p *parser) matchKeyword(word string) bool {
	p.skipSpace()
	if !p.matchKeywordAt(word) {
		return false
	}
	p.at += len(word)
	// Require word boundary.
	if p.at < len(p.input) {
		r, _ := utf8.DecodeRuneInString(p.input[p.at:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			p.at -= len(word)
			return false
		}
	}
	return true
}

func (p *parser) matchKeywordAt(word string) bool {
	upper := strings.ToUpper(p.input[p.at:])
	target := strings.ToUpper(word)
	if !strings.HasPrefix(upper, target) {
		return false
	}
	after := p.at + len(word)
	if after < len(p.input) {
		r, _ := utf8.DecodeRuneInString(p.input[after:])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isStringField(f field) bool {
	switch f {
	case fieldName, fieldNamespace, fieldStatus, fieldHealth:
		return true
	default:
		return false
	}
}

func fieldLabel(f field) string {
	switch f {
	case fieldName:
		return "name"
	case fieldNamespace:
		return "namespace"
	case fieldStatus:
		return "status"
	case fieldHealth:
		return "health"
	case fieldRestarts:
		return "restarts"
	case fieldAge:
		return "age"
	case fieldCPU:
		return "cpu"
	case fieldMemory:
		return "memory"
	default:
		return "field"
	}
}

func parseDuration(token string) (time.Duration, error) {
	if token == "" {
		return 0, fmt.Errorf("age requires a duration unit")
	}
	// Require a unit suffix for age.
	last := token[len(token)-1]
	if last >= '0' && last <= '9' {
		return 0, fmt.Errorf("age requires a duration unit")
	}
	d, err := time.ParseDuration(token)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", token)
	}
	if d < 0 {
		return 0, fmt.Errorf("age cannot be negative")
	}
	return d, nil
}

func parseInt(token string) (int64, error) {
	var n int64
	for _, ch := range token {
		if ch < '0' || ch > '9' {
			return 0, fmt.Errorf("invalid integer")
		}
		n = n*10 + int64(ch-'0')
	}
	return n, nil
}
