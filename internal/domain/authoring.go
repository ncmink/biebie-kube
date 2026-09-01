package domain

// This file describes authoring a Kubernetes object that does not exist yet.
//
// It is deliberately a different vocabulary from editing one that does. An
// edit has a live object to compare against, a resourceVersion to guard the
// write, and an owner to ask about. A creation has none of those: there is
// nothing to diff, nothing to conflict with, and the only protection against
// writing over somebody's work is the API server refusing a name that is
// already taken. So the words here are Create rather than Apply, and nothing
// in this file is allowed to fall back to an update.
//
// The other boundary this file carries is GitOps. An object an Argo CD
// Application declares has a desired state in a repository, and writing it
// straight into the cluster puts the two out of step until the next reconcile
// quietly wins. Direct creation is therefore offered only where nothing was
// found claiming the target — which is not the same as proving nothing does.

// AuthoringMode is how a person writes the manifest.
type AuthoringMode string

// The two authoring surfaces.
const (
	// AuthoringYAML is Kubernetes YAML, typed directly. It needs nothing
	// installed on the machine, which is why it stays available when the
	// TypeScript runtime does not.
	AuthoringYAML AuthoringMode = "yaml"

	// AuthoringTypeScript is cdk8s. It never applies what was typed: the
	// TypeScript is synthesised into manifests, and the manifests are what is
	// validated and created.
	AuthoringTypeScript AuthoringMode = "typescript"
)

// ToolStatus is what was found when one executable was looked for.
//
// Path and Version are carried so Settings can show which copy answered.
// A machine with three Node installations and a GUI that can only see one of
// them is the situation this exists for, and a bare tick would hide it.
type ToolStatus struct {
	Available bool `json:"available"`

	Path    string `json:"path,omitempty"`
	Version string `json:"version,omitempty"`

	// Reason says why the tool is unavailable, in words that distinguish "not
	// on the PATH this application can see" from "not installed". A desktop
	// application started by launchd sees a narrower PATH than a terminal
	// does, and telling somebody to install what they already have is the
	// most annoying way to be wrong.
	Reason string `json:"reason,omitempty"`
}

// AuthoringRuntime is whether this machine can synthesise cdk8s TypeScript.
type AuthoringRuntime struct {
	Node  ToolStatus `json:"node"`
	Npm   ToolStatus `json:"npm"`
	Cdk8s ToolStatus `json:"cdk8s"`

	// TypeScript is true only when every tool above answered. It is computed
	// here rather than in the frontend so no screen can decide readiness by
	// reading a version string it half understands.
	TypeScript bool `json:"typescript"`

	// Prepared reports that the app-managed dependency workspace has been
	// installed. TypeScript authoring needs both this and TypeScript above.
	Prepared bool `json:"prepared"`

	// Reason explains an unready runtime in one sentence.
	Reason string `json:"reason,omitempty"`

	// YAML is always true. It is carried anyway so the frontend states the
	// rule from the backend rather than hard-coding "YAML always works",
	// which stops being true the day it stops being true.
	YAML bool `json:"yaml"`
}

// CreateAvailability is whether direct creation is offered for one target.
//
// Both halves are reported rather than one boolean, because they fail for
// different reasons and lead to different places: a GitOps-managed namespace
// sends somebody to a repository, and a missing runtime sends them to
// Settings.
type CreateAvailability struct {
	ClusterID string `json:"clusterId"`
	Namespace string `json:"namespace"`

	// Kind is the navigation kind the screen was on, and TargetKind is how a
	// manifest spells it — "networkpolicies" against "NetworkPolicy". Both are
	// carried because the screen labels itself with one and the starter
	// template is written in the other.
	Kind       string `json:"kind,omitempty"`
	TargetKind string `json:"targetKind,omitempty"`

	// Namespaced is what this cluster says about the kind. The screen reads it
	// to decide whether to show a namespace at all: printing "namespace —"
	// beside a Namespace is the UI inventing a field the object does not have.
	Namespaced bool `json:"namespaced"`

	// NeedsNamespace marks the refusal that is neither a GitOps one nor a
	// runtime one: a namespaced kind with no namespace chosen. It is separate
	// because it is the only refusal the person fixes without leaving the
	// resource list, and because until it is fixed the GitOps question has
	// nothing to match against and must not be answered.
	NeedsNamespace bool `json:"needsNamespace,omitempty"`

	// Allowed is direct creation being offered at all, which in this slice
	// means nothing was found claiming this target.
	Allowed bool `json:"allowed"`

	// Managed is the GitOps side of that answer, kept separate so the UI can
	// explain a refusal instead of hiding the button.
	Managed bool `json:"managed"`

	// App names the Application that claims the target, when one does.
	App *ArgoApp `json:"app,omitempty"`

	// Reason is the sentence shown either way. It never says operating
	// directly is correct: not finding an owner is not proof there is none.
	Reason string `json:"reason"`

	// Modes are the authoring surfaces offered. YAML is always among them
	// when Allowed; TypeScript only when the runtime is ready.
	Modes []AuthoringMode `json:"modes,omitempty"`

	Runtime AuthoringRuntime `json:"runtime"`
}

// ManifestProblem is one reason a manifest is not ready to create.
//
// Problems are values rather than a formatted list, because the UI shows them
// beside the resource they belong to and a joined string cannot be.
type ManifestProblem struct {
	// Resource is the index of the document this is about, or -1 for a
	// problem with the manifest as a whole.
	Resource int `json:"resource"`

	Message string `json:"message"`
}

// ManifestResource is one document a manifest declares.
type ManifestResource struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Namespace  string `json:"namespace,omitempty"`

	// Namespaced is what the cluster's discovery says about this kind, so a
	// namespace on a ClusterRole is caught as the mistake it is rather than
	// sent to an API server that will reject it less clearly.
	Namespaced bool `json:"namespaced"`

	// Known is false for a kind this cluster does not serve. It is separated
	// from Namespaced because "unknown kind" and "cluster-scoped" are
	// different answers and both leave Namespaced false.
	Known bool `json:"known"`

	// Exists reports the object already being in the cluster. Creation is
	// refused rather than turned into an update.
	Exists bool `json:"exists"`
}

// ManifestPreview is a parsed, checked manifest waiting for a decision.
type ManifestPreview struct {
	// YAML is the manifest exactly as it will be sent: what was typed for the
	// YAML mode, and what cdk8s synthesised for the TypeScript one. The thing
	// reviewed and the thing created are the same text.
	YAML string `json:"yaml"`

	Resources []ManifestResource `json:"resources,omitempty"`
	Problems  []ManifestProblem  `json:"problems,omitempty"`

	// Ready is true when nothing found here prevents creation. It is not a
	// promise the API server will accept the manifest.
	Ready bool `json:"ready"`

	// Output is what cdk8s wrote while synthesising, kept for a person
	// reading a failure. It is scrubbed of the environment it ran in.
	Output string `json:"output,omitempty"`
}

// CreatedResource is one object the cluster accepted.
type CreatedResource struct {
	Ref  ResourceRef `json:"ref"`
	Kind string      `json:"kind"`
}

// CreateFailure is one object the cluster refused.
type CreateFailure struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Error     string `json:"error"`
}

// CreateOutcome is what happened, reported exactly.
//
// Partial success is a real outcome and is named as one. Kubernetes has no
// transaction across objects, so a manifest of three that fails on the third
// leaves two behind — and claiming otherwise, or claiming a rollback that was
// never implemented, would be worse than the partial state itself.
type CreateOutcome struct {
	Created []CreatedResource `json:"created,omitempty"`
	Failed  []CreateFailure   `json:"failed,omitempty"`

	// Message is the sentence the UI shows, written here because only this
	// side knows whether anything was created before the failure.
	Message string `json:"message"`
}
