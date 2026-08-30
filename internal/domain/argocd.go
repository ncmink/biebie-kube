package domain

import "time"

// The Argo CD kinds, named through CustomKind so a card on the dashboard and
// an entry in the sidebar address the same thing. They are custom resources
// like any other: nothing here is served unless the cluster's own definitions
// declare it.
var (
	KindArgoApplication    = CustomKind("applications", "argoproj.io")
	KindArgoApplicationSet = CustomKind("applicationsets", "argoproj.io")
	KindArgoProject        = CustomKind("appprojects", "argoproj.io")
)

// Argo CD reports two status axes and they disagree often enough to matter, so
// both are carried as the words Argo CD itself uses rather than collapsed into
// one traffic light on this side of the binding.
const (
	ArgoSynced    = "Synced"
	ArgoOutOfSync = "OutOfSync"

	ArgoHealthy     = "Healthy"
	ArgoProgressing = "Progressing"
	ArgoDegraded    = "Degraded"
	ArgoSuspended   = "Suspended"
	ArgoMissing     = "Missing"
)

// ArgoDashboard is the Argo CD entry in the cluster navigator.
//
// Every part is best-effort, for the same reason the cluster overview is: an
// account scoped to one namespace can read its Applications and not the
// repository Secrets, and that must render a useful page rather than an error.
type ArgoDashboard struct {
	ClusterID string `json:"clusterId"`

	// Installed is false when the cluster serves no Application definition. It
	// is the difference between "Argo CD manages nothing here" and "Argo CD is
	// not installed here", which are answers to different questions.
	Installed bool `json:"installed"`

	// Namespace is where argocd-server was found, which is not assumed to be
	// "argocd" — the label is what identifies it.
	Namespace string `json:"namespace,omitempty"`

	Summary ArgoSummary `json:"summary"`
	Cards   []ArgoCard  `json:"cards,omitempty"`

	NeedsAttention []ArgoApp      `json:"needsAttention,omitempty"`
	Activity       []ArgoActivity `json:"activity,omitempty"`
}

// ArgoSummary is the health strip: counts across every Application in the
// cluster.
type ArgoSummary struct {
	Applications int `json:"applications"`

	Synced    int `json:"synced"`
	OutOfSync int `json:"outOfSync"`

	Healthy     int `json:"healthy"`
	Progressing int `json:"progressing"`
	Degraded    int `json:"degraded"`
	Missing     int `json:"missing"`
}

// ArgoCard is one resource-kind card.
type ArgoCard struct {
	// Kind is the navigable kind behind the card, empty for a card the
	// sidebar has no list view for. Repositories and clusters are Secrets
	// wearing a label, and a Secrets list filtered to neither is not the view
	// the card promises.
	Kind  Kind   `json:"kind,omitempty"`
	Title string `json:"title"`

	Total int `json:"total"`

	// Healthy leads the Applications card, which reads healthy over total
	// rather than a bare count: the total alone says nothing worth a card.
	Healthy int  `json:"healthy"`
	Leads   bool `json:"leads,omitempty"`

	Chips []ArgoChip `json:"chips,omitempty"`
}

// ArgoChip is one problem count on a card.
type ArgoChip struct {
	Label  string `json:"label"`
	Count  int    `json:"count"`
	Health Health `json:"health"`
}

// ArgoApp is one Application, as the dashboard and the action dialogs list it.
type ArgoApp struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`

	Sync         string `json:"sync,omitempty"`
	HealthStatus string `json:"healthStatus,omitempty"`

	// Health is the traffic light the two statuses reduce to, so an Argo CD
	// row wears the same dot as every other row in the application.
	Health Health `json:"health"`

	Project string `json:"project,omitempty"`

	// Reason says why an Application needs attention, preferring Argo CD's own
	// message over the status word it repeats.
	Reason string `json:"reason,omitempty"`
}

// ArgoActivityKind categorises one line of the activity timeline.
type ArgoActivityKind string

// Activity categories.
const (
	ArgoActivityInfo     ArgoActivityKind = "info"
	ArgoActivityProgress ArgoActivityKind = "progress"
	ArgoActivitySuccess  ArgoActivityKind = "success"
	ArgoActivityFailure  ArgoActivityKind = "failure"
)

// ArgoActivity is one entry in the Recent activity timeline, built from
// Kubernetes events rather than from Argo CD's own API.
type ArgoActivity struct {
	UID string `json:"uid"`

	Category ArgoActivityKind `json:"category"`

	Reason  string `json:"reason"`
	Object  string `json:"object"`
	Message string `json:"message"`

	Namespace string    `json:"namespace,omitempty"`
	At        time.Time `json:"at"`
}

// ArgoAppRef addresses one Application for a sync or a refresh.
type ArgoAppRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// ArgoSyncRequest asks Argo CD to bring Applications in line with Git.
type ArgoSyncRequest struct {
	Apps []ArgoAppRef `json:"apps"`

	// Prune deletes live resources that no longer exist in the target
	// revision. It defaults to off and the dialog says what it does, because
	// the destructive half of a sync is the half nobody reads about
	// afterwards.
	Prune bool `json:"prune"`
}

// ArgoRefreshRequest asks Argo CD to compare Applications against Git again.
type ArgoRefreshRequest struct {
	Apps []ArgoAppRef `json:"apps"`

	// Hard discards the manifest cache as well, for when a normal refresh does
	// not pick up a change that was definitely pushed.
	Hard bool `json:"hard"`
}

// ArgoActionResult is one answer for a whole batch.
//
// A bulk sync over forty Applications that fails on two is not a failure and
// not a success, so both halves are reported and the UI raises one notice
// rather than forty.
type ArgoActionResult struct {
	Succeeded []string           `json:"succeeded,omitempty"`
	Failed    []ArgoActionFailed `json:"failed,omitempty"`
}

// ArgoActionFailed names one Application the action did not reach.
type ArgoActionFailed struct {
	App   string `json:"app"`
	Error string `json:"error"`
}

// ArgoEndpoint is a local URL that reaches the Argo CD web UI.
type ArgoEndpoint struct {
	URL string `json:"url"`

	// Reused reports that an existing port forward was good enough, so the
	// panel does not gain a second row for the same service.
	Reused bool `json:"reused,omitempty"`
}
