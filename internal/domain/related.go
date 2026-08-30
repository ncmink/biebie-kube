package domain

// RelatedGroup is one set of objects that belong to another object: the pods a
// deployment runs, the replica sets behind its revisions, the workload a pod
// came from.
//
// It carries its own columns rather than borrowing the catalogue's, because a
// group sometimes has one the list view does not — a replica set shown as a
// deployment's revision leads with the revision number, which means nothing on
// the replica sets page.
type RelatedGroup struct {
	Kind  Kind   `json:"kind"`
	Title string `json:"title"`

	// Namespaced tells the UI whether the namespace is worth a column. A
	// deployment's pods all share its namespace and would fill one with the
	// same word; a node's pods come from everywhere.
	Namespaced bool `json:"namespaced"`

	Columns []Column      `json:"columns"`
	Rows    []ResourceRow `json:"rows"`

	// Truncated marks a group that hit its budget, so the UI can say the list
	// is a prefix instead of presenting it as the whole set.
	Truncated bool `json:"truncated,omitempty"`
}
