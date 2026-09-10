package domain

import "time"

// IncidentConfidenceClass says how strongly a finding is supported by evidence.
type IncidentConfidenceClass string

const (
	IncidentConfidenceConfirmed IncidentConfidenceClass = "confirmed"
	IncidentConfidenceSupported IncidentConfidenceClass = "supported"
	IncidentConfidenceUnknown   IncidentConfidenceClass = "unknown"
)

// FindingSeverity orders findings in the UI.
type FindingSeverity string

const (
	SeverityCritical FindingSeverity = "critical"
	SeverityWarning  FindingSeverity = "warning"
	SeverityInfo     FindingSeverity = "informational"
)

// CoverageState reports how complete evidence collection was.
type CoverageState string

const (
	CoverageComplete CoverageState = "complete"
	CoveragePartial  CoverageState = "partial"
	CoverageUnknown  CoverageState = "unknown"
)

// IncidentReport is the Explain Why payload for one resource.
type IncidentReport struct {
	ReportID     string `json:"reportId"`
	ClusterID    string `json:"clusterId"`
	SessionEpoch string `json:"sessionEpoch"`

	RootRef ResourceRef `json:"rootRef"`
	RootUID string      `json:"rootUid"`

	CollectedAt time.Time        `json:"collectedAt"`
	Coverage    IncidentCoverage `json:"coverage"`
	Findings    []IncidentFinding `json:"findings"`
	Issues      []IncidentIssue   `json:"issues,omitempty"`
}

// IncidentCoverage describes what was read and what was missing.
type IncidentCoverage struct {
	State      CoverageState `json:"state"`
	ObservedAt time.Time     `json:"observedAt"`
	Scopes     []string      `json:"scopes,omitempty"`
	Missing    []string      `json:"missing,omitempty"`
}

// IncidentFinding is one evidence-backed explanation.
type IncidentFinding struct {
	RuleID          string          `json:"ruleId"`
	Severity        FindingSeverity `json:"severity"`
	Summary         string          `json:"summary"`
	Explanation     string          `json:"explanation"`
	ConfidenceClass IncidentConfidenceClass `json:"confidenceClass"`

	ObservedFacts  []string `json:"observedFacts,omitempty"`
	PossibleCauses []string `json:"possibleCauses,omitempty"`
	EvidenceIDs    []string `json:"evidenceIds,omitempty"`
	NextSteps      []string `json:"nextSteps,omitempty"`
}

// IncidentEvidence is one safe excerpt backing a finding.
type IncidentEvidence struct {
	ID         string      `json:"id"`
	SourceRef  ResourceRef `json:"sourceRef"`
	SourceUID  string      `json:"sourceUid,omitempty"`
	FieldPath  string      `json:"fieldPath,omitempty"`
	EventUID   string      `json:"eventUid,omitempty"`
	SafeExcerpt string     `json:"safeExcerpt"`
	ObservedAt time.Time   `json:"observedAt"`
}

// IncidentIssue records a collection problem without pretending the read succeeded.
type IncidentIssue struct {
	Scope   string `json:"scope"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
