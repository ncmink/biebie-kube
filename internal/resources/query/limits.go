package query

const (
	maxInputBytes = 2048
	maxTerms      = 20
	// metricsStaleAfter is when pod usage is too old for typed comparisons.
	metricsStaleAfter = 45 // seconds
)
