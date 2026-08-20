// Package cluster owns the clusters Biebie Kube knows about and the lifecycle
// of connecting to them.
//
// It depends on the kube package for Kubernetes access and on an access
// checker for customer connectivity, both through interfaces, so the lifecycle
// can be tested without a cluster or a VPN.
package cluster

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	bctx "biebie-kube/protocol/context"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/store"
)

// Repository stores cluster records.
type Repository struct {
	store *store.Store
	now   func() time.Time
}

// NewRepository wires the repository to persistent state.
func NewRepository(st *store.Store) *Repository {
	return &Repository{store: st, now: time.Now}
}

// All returns every cluster, grouped sensibly for the sidebar: customers in
// alphabetical order, and within a customer, production last so the most
// dangerous entry is never the one under the cursor by accident.
func (r *Repository) All() []domain.Cluster {
	records := r.store.Read().Clusters
	out := make([]domain.Cluster, 0, len(records))
	for _, record := range records {
		out = append(out, fromRecord(record))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CustomerName != out[j].CustomerName {
			return out[i].CustomerName < out[j].CustomerName
		}
		if out[i].IsProduction() != out[j].IsProduction() {
			return !out[i].IsProduction()
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Get returns one cluster.
func (r *Repository) Get(id string) (domain.Cluster, error) {
	for _, record := range r.store.Read().Clusters {
		if record.ID == id {
			return fromRecord(record), nil
		}
	}
	return domain.Cluster{}, fmt.Errorf("cluster %s does not exist", id)
}

// FindByContext locates a cluster by the identifiers a handoff carries.
//
// A handoff names a customer and a cluster; those identifiers come from Biebie
// Access, which does not know Biebie Kube's internal UUIDs. Matching by the
// context's cluster identifier first, then by customer and name, lets the two
// applications be configured independently.
func (r *Repository) FindByContext(ctx bctx.BiebieContext) (domain.Cluster, bool) {
	clusters := r.All()

	for _, cluster := range clusters {
		if cluster.ID == ctx.ClusterID {
			return cluster, true
		}
	}
	for _, cluster := range clusters {
		if labelMatch(cluster, "biebie.net/cluster-id", ctx.ClusterID) {
			return cluster, true
		}
	}
	for _, cluster := range clusters {
		if strings.EqualFold(cluster.CustomerID, ctx.CustomerID) &&
			strings.EqualFold(cluster.Name, ctx.ClusterName) {
			return cluster, true
		}
	}
	// A single cluster for that customer and environment is unambiguous even
	// when the names differ between the two applications.
	var candidates []domain.Cluster
	for _, cluster := range clusters {
		if strings.EqualFold(cluster.CustomerID, ctx.CustomerID) &&
			strings.EqualFold(cluster.EnvironmentID, ctx.EnvironmentID) {
			candidates = append(candidates, cluster)
		}
	}
	if len(candidates) == 1 {
		return candidates[0], true
	}
	return domain.Cluster{}, false
}

// Create stores a new cluster.
func (r *Repository) Create(in domain.ClusterInput, server string) (domain.Cluster, error) {
	if err := in.Validate(); err != nil {
		return domain.Cluster{}, err
	}

	now := r.now().UTC()
	record := toRecord(in, server)
	record.ID = "cluster_" + uuid.NewString()
	record.CreatedAt = now.Format(time.RFC3339)
	record.UpdatedAt = record.CreatedAt

	if err := r.store.Update(func(data *store.Data) error {
		for _, existing := range data.Clusters {
			if existing.KubeconfigRef == record.KubeconfigRef && existing.ContextName == record.ContextName {
				return fmt.Errorf("context %q is already added as %q", record.ContextName, existing.Name)
			}
		}
		data.Clusters = append(data.Clusters, record)
		return nil
	}); err != nil {
		return domain.Cluster{}, err
	}
	return fromRecord(record), nil
}

// Update edits a cluster.
func (r *Repository) Update(id string, in domain.ClusterInput, server string) (domain.Cluster, error) {
	if err := in.Validate(); err != nil {
		return domain.Cluster{}, err
	}

	var updated store.ClusterRecord
	err := r.store.Update(func(data *store.Data) error {
		for i, existing := range data.Clusters {
			if existing.ID != id {
				continue
			}
			record := toRecord(in, server)
			record.ID = existing.ID
			record.CreatedAt = existing.CreatedAt
			record.UpdatedAt = r.now().UTC().Format(time.RFC3339)

			// Whether a cluster is put away is not part of the form, and
			// labels are metadata an import or a handoff wrote rather than
			// something the form shows. Neither may be lost because someone
			// corrected a cluster's name.
			record.Archived = existing.Archived
			if record.Labels == nil {
				record.Labels = existing.Labels
			}
			data.Clusters[i] = record
			updated = record
			pruneCustomers(data)
			return nil
		}
		return fmt.Errorf("cluster %s does not exist", id)
	})
	if err != nil {
		return domain.Cluster{}, err
	}
	return fromRecord(updated), nil
}

// Delete forgets a cluster and its preferences. It removes Biebie's record
// only: the kubeconfig it referenced is untouched.
func (r *Repository) Delete(id string) error {
	return r.store.Update(func(data *store.Data) error {
		clusters := data.Clusters[:0]
		found := false
		for _, record := range data.Clusters {
			if record.ID == id {
				found = true
				continue
			}
			clusters = append(clusters, record)
		}
		if !found {
			return fmt.Errorf("cluster %s does not exist", id)
		}
		data.Clusters = clusters

		prefs := data.Preferences[:0]
		for _, pref := range data.Preferences {
			if pref.ClusterID != id {
				prefs = append(prefs, pref)
			}
		}
		data.Preferences = prefs
		pruneCustomers(data)
		return nil
	})
}

// Groups reports the cluster list as the dashboard shows it: one section per
// customer, named customers by label, the clusters without one after them, and
// the archive last.
//
// The archive is listed even when it holds nothing, because it is the
// destination the "put this away" action needs. A section with no clusters is
// not drawn, so an empty archive costs the dashboard nothing.
func (r *Repository) Groups() []domain.CustomerGroup {
	chosen := r.groupChoices()

	var groups []domain.CustomerGroup
	at := make(map[string]int)
	for _, cluster := range r.All() {
		key := cluster.GroupKey()
		if _, ok := at[key]; !ok {
			at[key] = len(groups)
			groups = append(groups, domain.CustomerGroup{
				Key:    key,
				Label:  cluster.GroupLabel(),
				Hidden: hiddenFor(chosen, key),
			})
		}
		index := at[key]
		groups[index].ClusterIDs = append(groups[index].ClusterIDs, cluster.ID)
	}
	if _, ok := at[domain.ArchivedKey]; !ok {
		groups = append(groups, domain.CustomerGroup{
			Key:    domain.ArchivedKey,
			Label:  domain.ArchivedLabel,
			Hidden: hiddenFor(chosen, domain.ArchivedKey),
		})
	}

	sort.SliceStable(groups, func(i, j int) bool {
		if groupRank(groups[i].Key) != groupRank(groups[j].Key) {
			return groupRank(groups[i].Key) < groupRank(groups[j].Key)
		}
		return strings.ToLower(groups[i].Label) < strings.ToLower(groups[j].Label)
	})
	return groups
}

// groupRank keeps the two sections that are not customers out of the alphabet:
// clusters nobody has claimed sit after the named customers, and the archive
// after those.
func groupRank(key string) int {
	switch key {
	case domain.ArchivedKey:
		return 2
	case "":
		return 1
	default:
		return 0
	}
}

// GroupHidden reports whether a section's clusters are kept off the list until
// the engineer asks for them.
func (r *Repository) GroupHidden(key string) bool {
	return hiddenFor(r.groupChoices(), strings.TrimSpace(key))
}

// SetGroupHidden hides or reveals one section of the cluster list.
//
// A choice that matches the section's default is recorded by removing the
// record rather than storing it, so state on disk only ever describes what
// differs. That is also what lets a revealed archive survive a restart while an
// untouched customer stores nothing at all.
//
// Nothing else changes: a hidden cluster still connects, one already connected
// keeps its session and its tab, and a handoff still resolves to it.
func (r *Repository) SetGroupHidden(key string, hidden bool) error {
	group := strings.TrimSpace(key)
	if !domain.CanHideGroup(group) {
		return fmt.Errorf("%s is not a customer that can be hidden", domain.UngroupedLabel)
	}
	return r.store.Update(func(data *store.Data) error {
		if group != domain.ArchivedKey && !customerInUse(data, group) {
			return fmt.Errorf("no cluster belongs to customer %q", group)
		}

		records := data.Customers[:0]
		for _, record := range data.Customers {
			if record.Key != group {
				records = append(records, record)
			}
		}
		data.Customers = records

		if hidden != domain.GroupHiddenByDefault(group) {
			data.Customers = append(data.Customers, store.CustomerRecord{Key: group, Hidden: hidden})
		}
		return nil
	})
}

// SetArchived puts a cluster in the archive or takes it back out.
//
// This is not an edit of the cluster. The customer fields are left exactly as
// they are, so a cluster taken out of the archive returns to its own customer's
// section, and it is allowed while the cluster is connected because where a
// cluster is listed has nothing to do with how it is reached.
func (r *Repository) SetArchived(id string, archived bool) (domain.Cluster, error) {
	var updated store.ClusterRecord
	err := r.store.Update(func(data *store.Data) error {
		for i, existing := range data.Clusters {
			if existing.ID != id {
				continue
			}
			data.Clusters[i].Archived = archived
			data.Clusters[i].UpdatedAt = r.now().UTC().Format(time.RFC3339)
			updated = data.Clusters[i]
			pruneCustomers(data)
			return nil
		}
		return fmt.Errorf("cluster %s does not exist", id)
	})
	if err != nil {
		return domain.Cluster{}, err
	}
	return fromRecord(updated), nil
}

// groupChoices reads the visibility the engineer has chosen per section. A
// section that is absent has never been chosen for and keeps its default.
func (r *Repository) groupChoices() map[string]bool {
	records := r.store.Read().Customers
	out := make(map[string]bool, len(records))
	for _, record := range records {
		out[record.Key] = record.Hidden
	}
	return out
}

// hiddenFor resolves a section's visibility: what the engineer chose, otherwise
// the section's default. A section that cannot be hidden at all is never
// reported hidden, so a flag stored by an older build cannot strand its
// clusters somewhere with no way back.
func hiddenFor(chosen map[string]bool, key string) bool {
	if !domain.CanHideGroup(key) {
		return false
	}
	if hidden, ok := chosen[key]; ok {
		return hidden
	}
	return domain.GroupHiddenByDefault(key)
}

// customerInUse asks whether any cluster still names this customer, archived or
// not. An archived cluster counts: it is out of the list, not out of the
// customer, so taking it back out finds the customer's own visibility intact.
func customerInUse(data *store.Data, key string) bool {
	for _, record := range data.Clusters {
		if fromRecord(record).CustomerKey() == key {
			return true
		}
	}
	return false
}

// pruneCustomers forgets presentation state for customers with no clusters
// left, so a customer that was renamed or whose last cluster was deleted cannot
// leave a hidden flag behind that silently applies when that identifier is
// typed again months later.
//
// The archive is exempt: it is a fixture of the list rather than an identifier
// someone happened to reuse, so a revealed archive stays revealed after its
// last cluster leaves.
func pruneCustomers(data *store.Data) {
	if len(data.Customers) == 0 {
		return
	}
	records := data.Customers[:0]
	for _, record := range data.Customers {
		if record.Key == domain.ArchivedKey || customerInUse(data, record.Key) {
			records = append(records, record)
		}
	}
	data.Customers = records
}

// Namespace returns the namespace last used for a cluster.
func (r *Repository) Namespace(clusterID string) string {
	for _, pref := range r.store.Read().Preferences {
		if pref.ClusterID == clusterID {
			return pref.LastNamespace
		}
	}
	return ""
}

// RememberNamespace stores the namespace the engineer is working in, so
// returning to a cluster returns to the same place.
func (r *Repository) RememberNamespace(clusterID, namespace string) error {
	return r.store.Update(func(data *store.Data) error {
		for i, pref := range data.Preferences {
			if pref.ClusterID == clusterID {
				data.Preferences[i].LastNamespace = namespace
				return nil
			}
		}
		data.Preferences = append(data.Preferences, store.PreferenceRecord{
			ClusterID:     clusterID,
			LastNamespace: namespace,
		})
		return nil
	})
}

func labelMatch(cluster domain.Cluster, key, value string) bool {
	if value == "" || cluster.Labels == nil {
		return false
	}
	return strings.EqualFold(cluster.Labels[key], value)
}

func toRecord(in domain.ClusterInput, server string) store.ClusterRecord {
	return store.ClusterRecord{
		Name:            strings.TrimSpace(in.Name),
		CustomerID:      strings.TrimSpace(in.CustomerID),
		CustomerName:    strings.TrimSpace(in.CustomerName),
		EnvironmentID:   strings.TrimSpace(in.EnvironmentID),
		EnvironmentName: strings.TrimSpace(in.EnvironmentName),
		EnvironmentKind: string(in.EnvironmentKind),
		Server:          strings.TrimSpace(server),
		KubeconfigRef:   in.KubeconfigRef,
		ContextName:     in.ContextName,
		RequiresAccess:  in.RequiresAccess,
		AccessProfileID: strings.TrimSpace(in.AccessProfileID),
		Labels:          in.Labels,
	}
}

func fromRecord(record store.ClusterRecord) domain.Cluster {
	cluster := domain.Cluster{
		ID:              record.ID,
		Name:            record.Name,
		CustomerID:      record.CustomerID,
		CustomerName:    record.CustomerName,
		EnvironmentID:   record.EnvironmentID,
		EnvironmentName: record.EnvironmentName,
		EnvironmentKind: bctx.Environment(record.EnvironmentKind),
		Server:          record.Server,
		KubeconfigRef:   record.KubeconfigRef,
		ContextName:     record.ContextName,
		Access: domain.AccessRequirement{
			Required:  record.RequiresAccess,
			ProfileID: record.AccessProfileID,
		},
		Archived: record.Archived,
		Labels:   record.Labels,
	}
	if parsed, err := time.Parse(time.RFC3339, record.CreatedAt); err == nil {
		cluster.CreatedAt = parsed
	}
	if parsed, err := time.Parse(time.RFC3339, record.UpdatedAt); err == nil {
		cluster.UpdatedAt = parsed
	}
	return cluster
}
