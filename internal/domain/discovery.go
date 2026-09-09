package domain

import "time"

// MetadataSource marks where a navigable kind's metadata came from.
type MetadataSource string

const (
	MetadataBuiltin    MetadataSource = "builtin"
	MetadataCRD        MetadataSource = "crd"
	MetadataDiscovery  MetadataSource = "discovery"
)

// DiscoveryIssue reports one partial-discovery or enrichment failure.
type DiscoveryIssue struct {
	Group   string `json:"group,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DiscoveryResource is one listable type the cluster serves.
type DiscoveryResource struct {
	Group            string         `json:"group"`
	Version          string         `json:"version"`
	Resource         string         `json:"resource"`
	Kind             string         `json:"kind"`
	Namespaced       bool           `json:"namespaced"`
	SupportedVerbs   []string       `json:"supportedVerbs,omitempty"`
	PreferredVersion string         `json:"preferredVersion,omitempty"`
	MetadataSource   MetadataSource `json:"metadataSource"`
}

// DiscoverySnapshot is the catalogue discovery result for one session.
type DiscoverySnapshot struct {
	ClusterID    string              `json:"clusterId"`
	SessionEpoch string              `json:"sessionEpoch"`
	Resources    []DiscoveryResource `json:"resources"`
	Issues       []DiscoveryIssue    `json:"issues,omitempty"`
	ObservedAt   time.Time           `json:"observedAt"`
	Complete     bool                `json:"complete"`
}

// ListAccess reports how a resource list may be read for one view.
type ListAccess string

const (
	ListAccessLive      ListAccess = "live"
	ListAccessSnapshot  ListAccess = "snapshot"
	ListAccessForbidden ListAccess = "forbidden"
)
