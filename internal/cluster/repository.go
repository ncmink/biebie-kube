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

	bctx "biebie.net/protocol/context"

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
			data.Clusters[i] = record
			updated = record
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
		return nil
	})
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
		Labels: record.Labels,
	}
	if parsed, err := time.Parse(time.RFC3339, record.CreatedAt); err == nil {
		cluster.CreatedAt = parsed
	}
	if parsed, err := time.Parse(time.RFC3339, record.UpdatedAt); err == nil {
		cluster.UpdatedAt = parsed
	}
	return cluster
}
