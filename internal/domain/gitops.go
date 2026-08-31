package domain

// This file carries the boundary the product is built around: a live object in
// a cluster is operational state, and the manifest behind it is desired state.
// Argo CD is the bridge, and these types are how far across that bridge this
// application can honestly see.
//
// Nothing here claims a mapping it cannot support. Every answer says how it was
// reached, because the difference between "Argo CD tracks this object" and
// "something labelled this object with an Application's name" is the difference
// between editing the right file and editing a file that has nothing to do
// with what is running.

// OwnershipConfidence says how firmly a live object is tied to the Argo CD
// Application that manages it.
type OwnershipConfidence string

// Ownership confidence levels, weakest first.
const (
	// OwnershipUnmanaged means no Application claims the object. Operating on
	// it directly is the correct thing to do: there is no desired state to
	// edit instead.
	OwnershipUnmanaged OwnershipConfidence = "unmanaged"

	// OwnershipCandidate means an Application's name was found on the object
	// but the Application does not list the object among its resources.
	//
	// This is the weak case and it is common: `app.kubernetes.io/instance` is
	// a standard Kubernetes label that Helm sets on everything it installs,
	// so finding it proves nothing on its own.
	OwnershipCandidate OwnershipConfidence = "candidate"

	// OwnershipConfirmed means the Application lists this object in its own
	// resource tree, which is Argo CD's own account of what it manages.
	OwnershipConfirmed OwnershipConfidence = "confirmed"

	// OwnershipTracked means Argo CD's tracking annotation names the
	// Application and matches this object's kind and name. This is the
	// strongest statement available without asking Argo CD's API.
	OwnershipTracked OwnershipConfidence = "tracked"
)

// Managed reports whether anything at all claims the object.
func (c OwnershipConfidence) Managed() bool {
	return c != "" && c != OwnershipUnmanaged
}

// ManifestCertainty says how precisely the desired state behind a live object
// can be located in Git.
//
// The honest answer is usually ManifestTree. Knowing the repository and the
// directory is not the same as knowing the file, and a directory of manifests
// rendered through Helm or Kustomize may contain no file resembling the object
// at all.
type ManifestCertainty string

// Manifest certainty levels, weakest first.
const (
	// ManifestUnknown means the source says nothing useful about where a
	// manifest would be — a multi-source value reference, or a source this
	// application does not understand.
	ManifestUnknown ManifestCertainty = "unknown"

	// ManifestGenerated means the manifests are rendered rather than stored:
	// Helm, Kustomize or a config-management plugin. A file exists that
	// influences this object, but no file equals it.
	ManifestGenerated ManifestCertainty = "generated"

	// ManifestTree means the repository, revision and directory are known and
	// the directory holds plain manifests. Which file in it declares this
	// object is not known until the tree is read.
	ManifestTree ManifestCertainty = "tree"

	// ManifestExact means one file is known to declare this object. Nothing
	// reaches this level yet: it requires reading the repository, which is a
	// later slice.
	ManifestExact ManifestCertainty = "exact"
)

// GitRenderer is what turns a source into Kubernetes manifests.
type GitRenderer string

// Renderers Argo CD supports.
const (
	RendererDirectory GitRenderer = "directory"
	RendererHelm      GitRenderer = "helm"
	RendererKustomize GitRenderer = "kustomize"
	RendererPlugin    GitRenderer = "plugin"

	// RendererChart is a chart pulled from a Helm repository rather than from
	// Git. There is no repository to browse and no file to edit, which is a
	// different answer from "the file is hard to find".
	RendererChart GitRenderer = "chart"
)

// GitSource is one place an Argo CD Application says its desired state comes
// from.
//
// An Application may declare several. Which of them produced any particular
// object is not something Argo CD records, so this is a list rather than a
// single value and the UI is expected to say so.
type GitSource struct {
	// RepoURL is the remote with any embedded credentials removed. It is
	// never the raw value from the Application: a repository URL may carry a
	// token in its userinfo, and that must not cross this boundary.
	RepoURL string `json:"repoUrl,omitempty"`

	// Repository is the owner-and-name part, for display: "acme/platform-infra".
	Repository string `json:"repository,omitempty"`
	Host       string `json:"host,omitempty"`

	Revision string `json:"revision,omitempty"`
	Path     string `json:"path,omitempty"`

	// Chart is set when the source is a Helm chart from a Helm repository.
	Chart string `json:"chart,omitempty"`

	// Ref names a source other sources refer to for values. A source with a
	// ref and no path produces no manifests of its own.
	Ref string `json:"ref,omitempty"`

	Renderer  GitRenderer       `json:"renderer"`
	Certainty ManifestCertainty `json:"certainty"`

	// Note explains the certainty in the words the UI shows, so the reason a
	// file cannot be named is written once here rather than reconstructed in
	// the frontend.
	Note string `json:"note,omitempty"`
}

// ResourceOwnership answers "where does this object's desired state live?".
type ResourceOwnership struct {
	Ref ResourceRef `json:"ref"`

	// Installed reports whether the cluster runs Argo CD at all.
	//
	// A cluster without it has no GitOps story to tell, which is a different
	// silence from an object Argo CD declines to manage: the first is not
	// worth a panel and the second is worth saying out loud.
	Installed bool `json:"installed"`

	Confidence OwnershipConfidence `json:"confidence"`

	// Reason says how the confidence was reached, in a sentence the UI shows.
	// A guess that explains itself can be argued with; a guess that does not
	// gets believed.
	Reason string `json:"reason"`

	// App is the Application that manages the object, absent when nothing
	// does.
	App *ArgoApp `json:"app,omitempty"`

	// AppKind is the navigable kind behind App, so the drawer can link to the
	// Application without the frontend having to spell out an API group. It
	// follows ArgoCard, which hands the sidebar its kind for the same reason.
	AppKind Kind `json:"appKind,omitempty"`

	// GeneratedBy names the ApplicationSet that produced the Application, when
	// one did. Editing desired state then means editing the generator or its
	// template rather than the Application.
	GeneratedBy string `json:"generatedBy,omitempty"`

	Sources []GitSource `json:"sources,omitempty"`
}

// ManifestLocation is one document in one file that declares the object.
//
// The document index matters as much as the path: a great many repositories
// keep a deployment, its service and its config map in one file separated by
// `---`, and "the file" is not the same answer as "the part of the file".
type ManifestLocation struct {
	Repository string `json:"repository,omitempty"`
	Path       string `json:"path"`
	Document   int    `json:"document"`

	// Content is the one document, not the whole file. It is carried only for
	// the location that was settled on: sending the text of every candidate
	// would move a directory of manifests across the binding to show a list of
	// file names.
	Content string `json:"content,omitempty"`
}

// ManifestSearch is what looking in the repository turned up.
//
// It is a separate answer from ResourceOwnership because it costs a clone
// where ownership costs a list, and because it can fail in ways ownership
// cannot — git may be absent, a host unreachable, a credential missing. None
// of those failures take away what ownership already established, so this
// arrives on its own and the panel keeps what it had.
type ManifestSearch struct {
	Ref ResourceRef `json:"ref"`

	Certainty ManifestCertainty `json:"certainty"`

	// Reason says what happened, whether or not it worked. A search that found
	// nothing and a search that could not run are both worth a sentence, and
	// they are not the same sentence.
	Reason string `json:"reason"`

	// Commit is the commit every file was read at, rather than the branch name
	// that was asked for. `main` is a different tree tomorrow, and an answer
	// that names its commit is one that can be checked afterwards.
	Commit string `json:"commit,omitempty"`

	// Located is the single document that declares this object, set only when
	// exactly one was found. Two candidates are not a location.
	Located *ManifestLocation `json:"located,omitempty"`

	// Candidates are every document that could be this object, listed when
	// there is more than one so the choice is the reader's rather than a
	// coin toss this code made quietly.
	Candidates []ManifestLocation `json:"candidates,omitempty"`

	// Scanned is how many files were read, and Truncated says the directory
	// held more than the budget allowed. A search that stopped early and found
	// nothing has not proved anything.
	Scanned   int  `json:"scanned"`
	Truncated bool `json:"truncated,omitempty"`
}
