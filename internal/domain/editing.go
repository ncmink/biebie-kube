package domain

import "time"

// This file describes editing a live object, which is a different question
// from comparing one with Git and a different question again from authoring
// one that does not exist.
//
// The distinction the types here exist to keep is between two comparisons that
// are easy to confuse and mean opposite things:
//
//	Source vs Live      the manifest in a repository, against the cluster
//	Original vs Edited  the cluster as it was when this editor opened,
//	                    against what the person has typed since
//
// The first says whether the cluster has drifted from what a repository
// declares. The second says what one person is about to do. Nothing in this
// file reads a repository, and nothing in gitops.go answers for an editor.

// EditSession is a live object captured for editing.
//
// Original is a snapshot and stays one. It is the whole meaning of the
// comparison the editor shows: "what have I changed since I opened this?"
// cannot be answered by a value that moves when a controller writes to the
// object. A live change while the editor is open is a staleness problem, is
// reported as one through EditFreshness, and never rewrites this field.
type EditSession struct {
	Ref ResourceRef `json:"ref"`

	// Original is the object as YAML at the moment the editor opened, cleaned
	// of the server-managed fields the same way the editor's read is.
	Original string `json:"original"`

	// ResourceVersion is the live object's version at that same moment. It is
	// the concurrency token: the write carries it back, so an object something
	// else changed in between is rejected by the API server rather than
	// overwritten.
	//
	// It is deliberately not left in Original. A resourceVersion in an editable
	// manifest is a field somebody can delete by accident, and the protection
	// would go with it.
	ResourceVersion string `json:"resourceVersion"`

	OpenedAt time.Time `json:"openedAt"`

	// Gate is whether a persistent change to this object may be written
	// directly, answered when the editor opened. The editor itself opens
	// regardless: reading an object is not a mutation, and an ownership check
	// that failed must not take away the ability to look at YAML.
	Gate MutationGate `json:"gate"`
}

// EditComparison is Original held against Edited.
//
// Two questions are answered rather than one, because they come apart. Dirty
// is whether the text differs, which is what a "modified" marker means to the
// person typing. Equivalent is whether the two parse to the same object, which
// is what decides whether applying would do anything — reordered keys and
// changed indentation are a dirty editor and an identical Kubernetes object.
type EditComparison struct {
	Dirty bool `json:"dirty"`

	// Equivalent is true when both sides parse and describe the same object.
	// It is false whenever Edited does not parse, because nothing can be said
	// about the meaning of text that is not YAML.
	Equivalent bool `json:"equivalent"`

	Added   int `json:"added"`
	Removed int `json:"removed"`

	// Hunks is how many separate places the two differ, which is the number
	// the editor shows. Lines are counted too, because "3 changes" over 300
	// added lines and over 3 is not the same review.
	Hunks int `json:"hunks"`

	// Invalid is why Edited could not be parsed, empty when it could. It is
	// reported rather than raised: text mid-edit is invalid most of the time,
	// and an editor that treated that as an error would shout constantly.
	Invalid string `json:"invalid,omitempty"`
}

// EditFreshness reports whether the live object moved since the editor opened.
//
// This is asked before a write rather than watched during one. An editor that
// followed the live object would be rewriting the thing the person is
// comparing against, and the comparison is the feature.
type EditFreshness struct {
	// Stale is the live object having a different resourceVersion from the one
	// the session captured. It is not an error and not a reason to discard
	// anything: the edits are still the person's, and the choice of what to do
	// about them is theirs.
	Stale bool `json:"stale"`

	// Gone is the object no longer existing. A deleted object cannot be
	// updated, and saying "it changed" would send somebody looking for a diff.
	Gone bool `json:"gone"`

	// ResourceVersion is what the cluster holds now, empty when it could not
	// be read.
	ResourceVersion string `json:"resourceVersion,omitempty"`

	// Unchecked says the question could not be answered — the read failed, or
	// the session carried no version to compare against. It is separated from
	// Stale because "it has not changed" and "we could not tell" must not look
	// the same to the person about to press Apply.
	Unchecked string `json:"unchecked,omitempty"`

	Reason string `json:"reason,omitempty"`
}
