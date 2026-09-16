// Package views persists named resource table queries per cluster.
package views

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/store"
)

// Repository reads and writes saved views in the app store.
type Repository struct {
	store *store.Store
	now   func() time.Time
}

// NewRepository wires the repository to persistent state.
func NewRepository(st *store.Store) *Repository {
	return &Repository{store: st, now: time.Now}
}

// List returns every saved view for one cluster, newest first.
func (r *Repository) List(clusterID string) []domain.SavedView {
	clusterID = strings.TrimSpace(clusterID)
	records := r.store.Read().SavedViews
	out := make([]domain.SavedView, 0)
	for _, record := range records {
		if record.ClusterID != clusterID {
			continue
		}
		out = append(out, fromRecord(record))
	}
	sortNewestFirst(out)
	return out
}

// Get returns one saved view.
func (r *Repository) Get(clusterID, id string) (domain.SavedView, error) {
	record, ok := r.find(clusterID, id)
	if !ok {
		return domain.SavedView{}, fmt.Errorf("saved view %s does not exist", id)
	}
	return fromRecord(record), nil
}

// Save creates or updates one saved view.
func (r *Repository) Save(in domain.SavedViewInput) (domain.SavedView, error) {
	if err := validateInput(in); err != nil {
		return domain.SavedView{}, err
	}

	now := r.now().UTC()
	var saved store.SavedViewRecord
	err := r.store.Update(func(data *store.Data) error {
		if _, err := findCluster(data, in.ClusterID); err != nil {
			return err
		}

		record := toRecord(in)
		record.UpdatedAt = now.Format(time.RFC3339)

		if record.ID == "" {
			if countForCluster(data, in.ClusterID) >= domain.MaxSavedViewsPerCluster {
				return fmt.Errorf("this cluster already has the maximum of %d saved views", domain.MaxSavedViewsPerCluster)
			}
			record.ID = "view_" + uuid.NewString()
			data.SavedViews = append(data.SavedViews, record)
			saved = record
			return nil
		}

		for i, existing := range data.SavedViews {
			if existing.ClusterID != in.ClusterID || existing.ID != record.ID {
				continue
			}
			record.ID = existing.ID
			data.SavedViews[i] = record
			saved = record
			return nil
		}
		return fmt.Errorf("saved view %s does not exist", record.ID)
	})
	if err != nil {
		return domain.SavedView{}, err
	}
	return fromRecord(saved), nil
}

// Delete removes one saved view.
func (r *Repository) Delete(clusterID, id string) error {
	return r.store.Update(func(data *store.Data) error {
		views := data.SavedViews[:0]
		found := false
		for _, record := range data.SavedViews {
			if record.ClusterID == clusterID && record.ID == id {
				found = true
				continue
			}
			views = append(views, record)
		}
		if !found {
			return fmt.Errorf("saved view %s does not exist", id)
		}
		data.SavedViews = views
		return nil
	})
}

// DeleteForCluster removes every saved view when a cluster is forgotten.
func DeleteForCluster(data *store.Data, clusterID string) {
	if len(data.SavedViews) == 0 {
		return
	}
	views := data.SavedViews[:0]
	for _, record := range data.SavedViews {
		if record.ClusterID != clusterID {
			views = append(views, record)
		}
	}
	data.SavedViews = views
}

func (r *Repository) find(clusterID, id string) (store.SavedViewRecord, bool) {
	for _, record := range r.store.Read().SavedViews {
		if record.ClusterID == clusterID && record.ID == id {
			return record, true
		}
	}
	return store.SavedViewRecord{}, false
}

func validateInput(in domain.SavedViewInput) error {
	in.ClusterID = strings.TrimSpace(in.ClusterID)
	in.Title = strings.TrimSpace(in.Title)
	if in.ClusterID == "" {
		return fmt.Errorf("cluster is required")
	}
	if in.Title == "" {
		return fmt.Errorf("title is required")
	}
	if len([]rune(in.Title)) > domain.MaxSavedViewTitleLength {
		return fmt.Errorf("title must be at most %d characters", domain.MaxSavedViewTitleLength)
	}
	if strings.TrimSpace(string(in.Kind)) == "" {
		return fmt.Errorf("resource type is required")
	}
	return nil
}

func findCluster(data *store.Data, clusterID string) (store.ClusterRecord, error) {
	for _, record := range data.Clusters {
		if record.ID == clusterID {
			return record, nil
		}
	}
	return store.ClusterRecord{}, fmt.Errorf("cluster %s does not exist", clusterID)
}

func countForCluster(data *store.Data, clusterID string) int {
	count := 0
	for _, record := range data.SavedViews {
		if record.ClusterID == clusterID {
			count++
		}
	}
	return count
}

func toRecord(in domain.SavedViewInput) store.SavedViewRecord {
	return store.SavedViewRecord{
		ID:            strings.TrimSpace(in.ID),
		ClusterID:     strings.TrimSpace(in.ClusterID),
		Title:         strings.TrimSpace(in.Title),
		Kind:          string(in.Kind),
		Namespace:     strings.TrimSpace(in.Namespace),
		QueryVersion:  domain.SavedViewQueryVersion,
		Mode:          string(in.Mode),
		Filter:        strings.TrimSpace(in.Filter),
		Expression:    strings.TrimSpace(in.Expression),
		LabelSelector: strings.TrimSpace(in.LabelSelector),
		FieldSelector: strings.TrimSpace(in.FieldSelector),
		SortKey:       strings.TrimSpace(in.SortKey),
		SortDesc:      in.SortDesc,
		ColumnIDs:     append([]string(nil), in.ColumnIDs...),
	}
}

func fromRecord(record store.SavedViewRecord) domain.SavedView {
	view := domain.SavedView{
		ID:            record.ID,
		ClusterID:     record.ClusterID,
		Title:         record.Title,
		Kind:          domain.Kind(record.Kind),
		Namespace:     record.Namespace,
		QueryVersion:  record.QueryVersion,
		Mode:          domain.QueryMode(record.Mode),
		Filter:        record.Filter,
		Expression:    record.Expression,
		LabelSelector: record.LabelSelector,
		FieldSelector: record.FieldSelector,
		SortKey:       record.SortKey,
		SortDesc:      record.SortDesc,
		ColumnIDs:     append([]string(nil), record.ColumnIDs...),
	}
	if parsed, err := time.Parse(time.RFC3339, record.UpdatedAt); err == nil {
		view.UpdatedAt = parsed
	}
	return view
}

func sortNewestFirst(views []domain.SavedView) {
	for i := 0; i < len(views); i++ {
		for j := i + 1; j < len(views); j++ {
			if views[j].UpdatedAt.After(views[i].UpdatedAt) {
				views[i], views[j] = views[j], views[i]
			}
		}
	}
}
