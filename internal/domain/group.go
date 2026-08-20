package domain

// UngroupedLabel is the heading for clusters that name no customer, which is
// where auto-imported kubeconfig contexts start out.
const UngroupedLabel = "Ungrouped"

// ArchivedKey identifies the archive: the one section of the cluster list that
// is not a customer.
//
// Every other key is a customer identifier or a customer name someone typed,
// and neither can look like this, so the archive cannot collide with a real
// customer however the engineer names their clients.
const ArchivedKey = "biebie/archived"

// ArchivedLabel is the heading the archive is shown under.
const ArchivedLabel = "Archived"

// GroupHiddenByDefault reports the visibility a section has before anyone has
// chosen for it.
//
// Only the archive starts out of sight, which is the whole point of having it:
// there is always one place to put a cluster you are not working with, and it
// stays quiet without being configured first.
func GroupHiddenByDefault(key string) bool { return key == ArchivedKey }

// CanHideGroup reports whether a section can be kept off the cluster list.
//
// The clusters that name no customer cannot. "Ungrouped" is not a customer the
// engineer is done with for the day, it is where auto-import puts every context
// it finds, so hiding it would mean a freshly imported cluster arrives invisible
// and the section offering to reveal it is the one that just disappeared. A
// single cluster goes to the archive instead.
func CanHideGroup(key string) bool { return key != "" }

// CustomerGroup is one section of the cluster list: the clusters of a single
// customer, and whether that section is kept out of sight.
//
// This is not a second grouping bolted on top of clusters. The list has always
// been read customer-first; a group only adds the one thing the customer itself
// owns — whether an engineer who works for several companies has put the ones
// they are not on today away. Hiding is presentation: no cluster is deleted, a
// hidden cluster still connects, and a handoff from Biebie Access still finds
// it.
type CustomerGroup struct {
	Key   string `json:"key"`
	Label string `json:"label"`

	Hidden bool `json:"hidden"`

	// ClusterIDs carries the order the cluster list already sorted them into,
	// so the UI needs no second opinion about which cluster sits where.
	ClusterIDs []string `json:"clusterIds"`
}
