package incident

import (
	"time"

	"biebie-kube/internal/domain"
)

// EvidenceBundle is the bounded snapshot rules evaluate.
type EvidenceBundle struct {
	RootRef domain.ResourceRef
	RootUID string

	Pod        *domain.PodDetail
	Events     []domain.EventRow
	EventsOK   bool
	EventsErr  string
	Related    []domain.RelatedGroup
	Inspect    *domain.ResourceInspect
	Properties map[string]string

	Evidence []domain.IncidentEvidence
	Scopes   []string
	Missing  []string
	Issues   []domain.IncidentIssue
}

func (b *EvidenceBundle) addEvidence(item domain.IncidentEvidence) {
	b.Evidence = append(b.Evidence, item)
}

func (b *EvidenceBundle) noteScope(scope string) {
	b.Scopes = appendUnique(b.Scopes, scope)
}

func (b *EvidenceBundle) noteMissing(scope string) {
	b.Missing = appendUnique(b.Missing, scope)
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}

func coverageFrom(bundle EvidenceBundle) domain.IncidentCoverage {
	state := domain.CoverageComplete
	if len(bundle.Missing) > 0 {
		state = domain.CoveragePartial
	}
	if bundle.Pod == nil && bundle.Inspect == nil {
		state = domain.CoverageUnknown
	}
	return domain.IncidentCoverage{
		State:      state,
		ObservedAt: time.Now().UTC(),
		Scopes:     bundle.Scopes,
		Missing:    bundle.Missing,
	}
}
