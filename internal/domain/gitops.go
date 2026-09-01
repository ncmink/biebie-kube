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

	// ManifestExact means one document in one file is known to declare this
	// object, because the repository was read and exactly one thing in it
	// matched. This is the only level at which desired state can be compared
	// with live state.
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

	// Output is what git wrote when it refused, kept whole for the reader who
	// does not believe the sentence.
	//
	// Reason is one line lifted out of several by a rule that knows the
	// failures git had already grown by the time it was written. When git
	// fails some other way the sentence can be the wrong line, and a person
	// with a terminal open would rather see the output than argue with a
	// summary of it. It is empty whenever git was not the thing that refused.
	Output string `json:"output,omitempty"`

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

// DifferenceKind says which side of the comparison a field came from.
//
// The three are deliberately descriptive rather than diagnostic. A field the
// cluster carries and the manifest does not may be drift, or a default the API
// server filled in, or something a mutating webhook or a controller wrote —
// and this application cannot tell those apart without asking the cluster to
// render the manifest itself. So it reports where the field is, not whose
// fault it is.
type DifferenceKind string

// The three ways two objects can disagree about one field.
const (
	// DifferenceChanged is a field both sides declare, differently. This is
	// the one that usually means what it looks like it means.
	DifferenceChanged DifferenceKind = "changed"

	// DifferenceAddedInLive is a field the cluster has and the manifest does
	// not. Most of these are defaults rather than drift, which is why they are
	// shown separately instead of being counted as changes.
	DifferenceAddedInLive DifferenceKind = "addedInLive"

	// DifferenceMissingInLive is a field the manifest declares and the object
	// does not carry. This is the rarer and more interesting direction: it can
	// mean the manifest has not been applied.
	DifferenceMissingInLive DifferenceKind = "missingInLive"
)

// DifferenceClass says whether a difference is one to look at.
//
// It is not the same question as DifferenceKind. The kind says which side a
// field is on; the class says whether anybody needs to do anything about it.
// A comparison that answers only the first turns into a list of forty rows in
// which the one that matters is indistinguishable from the thirty-nine that do
// not, and a list like that is read once and then ignored.
type DifferenceClass string

// The two classes.
const (
	// DifferenceMeaningful is the default and stays the default. A field this
	// application cannot account for belongs in front of the reader: showing
	// one field too many costs a moment, and hiding one costs a drift nobody
	// notices.
	DifferenceMeaningful DifferenceClass = "meaningful"

	// DifferenceSystemManaged is a field Kubernetes, one of its controllers,
	// or Argo CD wrote after the manifest was applied. It really is different
	// and it is not evidence of anything, so it is kept and set aside rather
	// than counted or discarded.
	DifferenceSystemManaged DifferenceClass = "systemManaged"
)

// DifferenceGroup gathers system-managed differences under a heading, so the
// detail behind a disclosure reads as a few short lists rather than as the
// same flat dump the summary was hiding.
type DifferenceGroup string

// The groups a system-managed difference can fall into.
const (
	GroupNone DifferenceGroup = ""

	// GroupController is something that wrote to the object after it was
	// applied: the Deployment controller, a rollout restart, Argo CD's own
	// tracking.
	GroupController DifferenceGroup = "controller"
)

// The three concepts below are deliberately separate, and collapsing them is
// the mistake this part of the model exists to prevent.
//
// Normalisation says two representations mean the same thing, and removes the
// difference before anybody sees it. Classification says a difference exists
// and belongs to a known category. Explanation says a difference exists
// because a named controller owns the field or a named rule ignores it.
//
// Only the third is allowed to name a culprit, and only when the cluster was
// asked and answered. An explanation that cannot be reached from evidence is
// not written: "no controller ownership was identified" is a true sentence and
// "somebody probably scaled it" is not.

// DifferenceCause is what accounts for a difference, when anything does.
type DifferenceCause string

// The causes, in order of how much they settle.
const (
	// CauseUnknown is the default and stays the default. Nothing found in the
	// cluster accounts for the difference, which is not the same as saying
	// nothing does — it is the honest report of a search that came back empty.
	CauseUnknown DifferenceCause = "unknown"

	// CauseArgoIgnored is a field an Argo CD Application is configured to
	// ignore, with no controller found that would be writing it. It explains
	// why Argo CD can report Synced and explains nothing about the live value.
	CauseArgoIgnored DifferenceCause = "argoIgnored"

	// CauseController is another controller owning the field: a
	// HorizontalPodAutoscaler writing replicas, and later its equivalents. The
	// difference is the system working rather than drift.
	CauseController DifferenceCause = "controller"
)

// ExplanationConfidence says how firmly the evidence supports what is claimed.
//
// It is carried on each piece of evidence as well as on the explanation as a
// whole, because they are not the same statement: an HPA that targets this
// object is a confirmed fact about ownership, and a manager name read out of
// managedFields is a fact that supports it without establishing it.
type ExplanationConfidence string

// Confidence levels, weakest first.
const (
	// ConfidenceUnknown is evidence that settles nothing. An Argo CD ignore
	// rule with no controller behind it is this: real, checkable, and silent
	// about what writes the value.
	ConfidenceUnknown ExplanationConfidence = "unknown"

	// ConfidenceSupporting strengthens an explanation without establishing it.
	// managedFields is the example the distinction was written for.
	ConfidenceSupporting ExplanationConfidence = "supporting"

	// ConfidenceConfirmed is a fact read off an object in the cluster that
	// establishes the claim by itself, the way an HPA naming this Deployment
	// as its scale target establishes that something else owns replicas.
	ConfidenceConfirmed ExplanationConfidence = "confirmed"
)

// DifferenceAttention says whether an explained difference still wants a
// person, which is a different question from what caused it.
//
// They are separate fields because they genuinely come apart. Replicas owned
// by an autoscaler that Argo CD is not configured to ignore has a confirmed
// controller cause and is still worth somebody's afternoon, and marking it
// "explained, nothing to see" because the cause is known would be the panel
// talking a reader out of a real problem.
type DifferenceAttention string

// The attention levels.
const (
	// AttentionNone is a difference the cluster accounts for and nobody needs
	// to act on.
	AttentionNone DifferenceAttention = "none"

	// AttentionReview is worth reading. It is not an error and must not be
	// coloured as one: the two situations it covers — a controller Argo CD
	// does not ignore, and an ignore rule with no controller behind it — are
	// both things that are often deliberate.
	AttentionReview DifferenceAttention = "review"
)

// EvidenceKind names where one piece of evidence was read from, so the UI can
// group it and a reader can go and check it.
type EvidenceKind string

// The evidence this slice can produce.
const (
	// EvidenceHPATarget is a HorizontalPodAutoscaler whose scale target is
	// this object.
	EvidenceHPATarget EvidenceKind = "hpaTarget"

	// EvidenceArgoIgnore is an entry in the owning Application's
	// ignoreDifferences that applies to this object and this field.
	EvidenceArgoIgnore EvidenceKind = "argoIgnore"

	// EvidenceFieldOwner is a manager that metadata.managedFields records as
	// owning the differing field itself.
	EvidenceFieldOwner EvidenceKind = "fieldOwner"
)

// EvidenceFact is one raw value kept exactly as the cluster reported it.
//
// Facts are carried rather than folded into the summary sentence because the
// sentence is this application's wording and the numbers are the cluster's. An
// engineer who does not believe the sentence can read the numbers under it.
type EvidenceFact struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DifferenceEvidence is one checkable reason behind an explanation.
type DifferenceEvidence struct {
	Kind       EvidenceKind          `json:"kind"`
	Confidence ExplanationConfidence `json:"confidence"`

	// Summary is the one line the panel shows for this piece on its own.
	Summary string `json:"summary"`

	// Subject is what the evidence is about — the autoscaler's name, the
	// manager's name — so the UI can lead with it rather than parse it back
	// out of the sentence.
	Subject string `json:"subject,omitempty"`

	Facts []EvidenceFact `json:"facts,omitempty"`
}

// DifferenceExplanation is why a difference exists, when the cluster holds
// enough to say.
//
// It is structured rather than a sentence because the parts are read in a
// particular order — does this need attention, who manages the field, why the
// source differs, why Argo CD can still say Synced, and only then the
// technical detail — and a paragraph cannot be laid out that way.
type DifferenceExplanation struct {
	Cause      DifferenceCause       `json:"cause"`
	Confidence ExplanationConfidence `json:"confidence"`
	Attention  DifferenceAttention   `json:"attention"`

	// Summary is the sentence the panel leads with. It is written here rather
	// than in the frontend because what may honestly be said about an HPA is
	// Kubernetes knowledge, and Kubernetes knowledge in a Vue component is
	// Kubernetes knowledge nobody can test.
	Summary string `json:"summary"`

	// Note is a secondary sentence: true, but not the finding. An Application
	// with no ignoreDifferences rule for this field, while Argo CD still
	// reports Synced, is the example it exists for — saying that up front
	// would bury the HPA under a speculation about Argo CD.
	Note string `json:"note,omitempty"`

	// ManagedBy names the controller that owns the field, in the form a person
	// reads it: "HorizontalPodAutoscaler / super-report". Empty when no
	// controller was identified, which is a state the panel says out loud.
	ManagedBy string `json:"managedBy,omitempty"`

	// ApplicationIgnore is what this analysis can say about
	// spec.ignoreDifferences on the owning Application, for this field.
	//
	// applies — a rule covering the field was found
	// absent  — the Application was read and no applicable rule was found
	// unread  — the Application could not be read
	// empty   — Argo CD was not consulted
	//
	// Absent is not proof that Argo CD ignores nothing. Global ignore rules
	// and other normalisation were not inspected.
	ApplicationIgnore string `json:"applicationIgnore,omitempty"`

	Evidence []DifferenceEvidence `json:"evidence,omitempty"`

	// Unchecked names the questions this analysis could not answer: a list
	// that failed, a rule written in a form it does not evaluate.
	//
	// It exists so that an incomplete analysis cannot masquerade as a
	// complete one. A missing answer changes what may be claimed — an ignore
	// rule this code cannot read is not an ignore rule that is absent — and
	// the difference between the two is the whole value of the panel.
	Unchecked []string `json:"unchecked,omitempty"`
}

// StateDifference is one field where Git and the cluster disagree.
type StateDifference struct {
	// Path names the field the way a person would say it out loud, with keyed
	// list elements addressed by their key rather than by position:
	// `spec.template.spec.containers[name=api].image`.
	Path string         `json:"path"`
	Kind DifferenceKind `json:"kind"`

	Class DifferenceClass `json:"class"`
	Group DifferenceGroup `json:"group,omitempty"`

	// Label and Subject are the words a person uses for the field: "Container
	// image" for the image of the container named in Subject.
	//
	// They are decided here rather than in the frontend because working out
	// that `spec.template.spec.containers[name=api].image` is a container's
	// image is Kubernetes knowledge, and Kubernetes knowledge in a Vue
	// component is Kubernetes knowledge nobody can test. An empty label means
	// this application has no better name than the path, and the path is shown.
	Label   string `json:"label,omitempty"`
	Subject string `json:"subject,omitempty"`

	// Reason says who wrote a system-managed field, so setting it aside is
	// something the reader can check rather than something they have to trust.
	Reason string `json:"reason,omitempty"`

	// Source and Live are rendered values rather than the values themselves.
	//
	// A typed field crossing the binding would have to be `any`, and `any` is
	// what carries a whole Secret across a boundary the moment somebody diffs
	// one. A string is something this package can shorten, and can decline to
	// fill in at all.
	Source string `json:"source,omitempty"`
	Live   string `json:"live,omitempty"`

	// SourceImplicit marks a Source value the manifest does not contain: the
	// field is absent from the file, and Source is the value Kubernetes uses
	// when it is absent.
	//
	// It is not rendered as Git having declared that value. An omitted
	// Deployment replica count is often intentional — an autoscaler owns
	// scaling — and showing "implicit 1" would put a desire in the repository
	// that is not there. The panel says omitted, then the default, separately.
	SourceImplicit bool `json:"sourceImplicit,omitempty"`

	// Redacted marks a difference whose values were deliberately withheld. The
	// field differs, and what it differs by is not something the UI is told.
	Redacted bool `json:"redacted,omitempty"`

	// Explanation is what the cluster says accounts for this difference,
	// absent when nothing was found or when the analysis could not run.
	//
	// Absent is a meaningful state and is not an error: it is the difference
	// between "an autoscaler owns this field" and "nothing here accounts for
	// this", and the second is what the reader needs to see plainly.
	Explanation *DifferenceExplanation `json:"explanation,omitempty"`
}

// ComparisonState is the outcome of holding desired state against live state.
type ComparisonState string

// The three outcomes.
//
// "Comparable" is not among them on purpose: it describes a precondition
// rather than a result, and by the time this value exists the comparison has
// either happened or been refused.
const (
	// ComparisonUnavailable means no comparison was made. It is not an error
	// and must not be shown as one — a Helm-rendered Application has no file
	// to compare against and never will.
	ComparisonUnavailable ComparisonState = "unavailable"

	// ComparisonEqual means nothing meaningful differs. System-managed fields
	// may still differ, which is why the panel says "no meaningful
	// differences" rather than "identical": a Deployment carrying its
	// controller's revision annotation is not identical to its manifest and
	// never will be, and claiming otherwise would be the kind of small
	// untruth that costs a reader their trust in everything else on screen.
	ComparisonEqual ComparisonState = "equal"

	// ComparisonDiffers means at least one meaningful difference remains.
	// Some of those may be explained by a controller or an ignore rule; the
	// counts on this type say which.
	ComparisonDiffers ComparisonState = "differs"
)

// ComparisonBlocker says why a comparison did not happen.
//
// These are separate values rather than one message because they are genuinely
// different situations: a repository that could not be reached is worth trying
// again, and a Helm chart is not.
type ComparisonBlocker string

// Why there was nothing to compare.
const (
	BlockerNone ComparisonBlocker = ""

	// BlockerUnmanaged is an object no Argo CD Application claims. There is no
	// desired state, so there is nothing for the live state to differ from.
	BlockerUnmanaged ComparisonBlocker = "unmanaged"

	// BlockerGenerated is Helm, Kustomize, a plugin or a chart. A file exists
	// that influences the object; no file equals it. Rendering it is a later
	// slice and guessing at it would be worse than saying so.
	BlockerGenerated ComparisonBlocker = "generated"

	// BlockerNotLocated is a directory that was read and held nothing
	// declaring this object.
	BlockerNotLocated ComparisonBlocker = "notLocated"

	// BlockerAmbiguous is more than one document declaring it. Comparing
	// against whichever came first would be inventing an answer.
	BlockerAmbiguous ComparisonBlocker = "ambiguous"

	// BlockerRepository is git failing: not installed, not reachable, not
	// authenticated, or a transport this application will not run.
	BlockerRepository ComparisonBlocker = "repository"

	// BlockerManifestInvalid is a located document that does not parse as a
	// Kubernetes object. The file is named and the reason is given, because
	// this one is worth going and fixing.
	BlockerManifestInvalid ComparisonBlocker = "manifestInvalid"

	// BlockerGone is a live object that no longer exists. A deleted object is
	// not an object that matches its manifest, and collapsing the two would be
	// the most misleading thing this panel could do.
	BlockerGone ComparisonBlocker = "gone"
)

// StateComparison is what holding the manifest against the object showed.
type StateComparison struct {
	State   ComparisonState   `json:"state"`
	Blocker ComparisonBlocker `json:"blocker,omitempty"`

	// Reason is the sentence the panel shows, written here so the wording for
	// each blocker lives in one place rather than in a frontend switch.
	Reason string `json:"reason"`

	// Differences holds both classes, meaningful ones first. Nothing is thrown
	// away: a controller's annotation is still a fact about the object, and an
	// engineer working out why a rollout happened will want it.
	Differences []StateDifference `json:"differences,omitempty"`

	// Meaningful and SystemManaged are counted here so the summary line is one
	// value rather than something the frontend derives by filtering. They are
	// what the panel leads with, and a count computed in two places is a count
	// that eventually disagrees with itself.
	Meaningful    int `json:"meaningful"`
	SystemManaged int `json:"systemManaged"`

	// Explained and NeedsAttention split the meaningful differences by what
	// the cluster was able to say about them, and they are not complements of
	// each other: a difference can be explained by a controller and still want
	// a person, which is what makes "explained" and "fine" different words.
	//
	// Both are counted here for the same reason the two above are. The summary
	// line is one value rather than something the frontend derives by
	// filtering, because a count computed in two places is a count that
	// eventually disagrees with itself.
	Explained      int `json:"explained"`
	NeedsAttention int `json:"needsAttention"`

	// Redacted says at least one difference had its values withheld, so the
	// panel can explain the blanks rather than let them look like a bug.
	Redacted bool `json:"redacted,omitempty"`
}

// SourceState is the manifest in Git that declares this object, and how that
// manifest compares with what the cluster is running.
//
// Source rather than desired, deliberately. What this reads is a file in a
// repository, and between that file and the object there may be a Helm or
// Kustomize rendering, a pipeline that rewrites an image reference, or an Argo
// CD parameter override. The rendered desired state Argo CD actually applied is
// a thing this application does not have, and naming this type after it would
// be claiming a certainty that has to be earned by a later slice.
//
// The two halves arrive together because they must come from the same commit.
// Asking for the manifest and then asking for the comparison would be two reads
// of a branch that moves, and the panel could end up showing a file from one
// commit beside a difference computed against another.
type SourceState struct {
	Ref ResourceRef `json:"ref"`

	// Search is where the manifest is, or why it could not be found. Its
	// Commit is the commit the comparison was made at.
	Search ManifestSearch `json:"search"`

	Comparison StateComparison `json:"comparison"`
}
