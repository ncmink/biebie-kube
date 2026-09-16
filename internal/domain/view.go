package domain

import "time"

// Saved view limits from VIEW-05.
const (
	SavedViewQueryVersion      = 1
	MaxSavedViewsPerCluster    = 100
	MaxSavedViewTitleLength    = 80
)

// SavedView is a named resource table query persisted for one cluster.
type SavedView struct {
	ID        string `json:"id"`
	ClusterID string `json:"clusterId"`
	Title     string `json:"title"`
	Kind      Kind   `json:"kind"`

	// Namespace is the table scope. Empty means all namespaces.
	Namespace string `json:"namespace,omitempty"`

	QueryVersion int       `json:"queryVersion"`
	Mode         QueryMode `json:"mode,omitempty"`
	Filter       string    `json:"filter,omitempty"`
	Expression   string    `json:"expression,omitempty"`
	LabelSelector string   `json:"labelSelector,omitempty"`
	FieldSelector string   `json:"fieldSelector,omitempty"`
	SortKey      string    `json:"sortKey,omitempty"`
	SortDesc     bool      `json:"sortDesc,omitempty"`

	// ColumnIDs lists kind-specific columns to show. Empty means all columns.
	ColumnIDs []string `json:"columnIds,omitempty"`

	UpdatedAt time.Time `json:"updatedAt"`
}

// SavedViewInput creates or updates one saved view.
type SavedViewInput struct {
	ClusterID string `json:"clusterId"`
	ID        string `json:"id,omitempty"`
	Title     string `json:"title"`
	Kind      Kind   `json:"kind"`
	Namespace string `json:"namespace,omitempty"`

	Mode          QueryMode `json:"mode,omitempty"`
	Filter        string    `json:"filter,omitempty"`
	Expression    string    `json:"expression,omitempty"`
	LabelSelector string    `json:"labelSelector,omitempty"`
	FieldSelector string    `json:"fieldSelector,omitempty"`
	SortKey       string    `json:"sortKey,omitempty"`
	SortDesc      bool      `json:"sortDesc,omitempty"`
	ColumnIDs     []string  `json:"columnIds,omitempty"`
}

// SavedViewIssue identifies one unresolved part of a saved view.
type SavedViewIssue string

const (
	SavedViewIssueKindMissing      SavedViewIssue = "kind_missing"
	SavedViewIssueNamespaceMissing SavedViewIssue = "namespace_missing"
	SavedViewIssueColumnMissing    SavedViewIssue = "column_missing"
	SavedViewIssueQueryInvalid     SavedViewIssue = "query_invalid"
	SavedViewIssueUnknownFormat    SavedViewIssue = "unknown_format"
)

// SavedViewIssueDetail explains one unresolved issue in user-facing terms.
type SavedViewIssueDetail struct {
	Code    SavedViewIssue `json:"code"`
	Message string         `json:"message"`
}

// SavedViewResolution reports whether a saved view can be applied as stored.
type SavedViewResolution struct {
	View   SavedView            `json:"view"`
	Valid  bool                 `json:"valid"`
	Issues []SavedViewIssueDetail `json:"issues,omitempty"`
}
