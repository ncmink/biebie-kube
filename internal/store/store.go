// Package store persists Biebie Kube's own state — cluster records, kubeconfig
// references and UI preferences — as a single JSON document.
//
// What is deliberately absent: Kubernetes tokens, client certificates,
// kubeconfig bodies and anything else a cluster would accept as proof of
// identity. This file records where to find credentials, never the credentials
// themselves, so a copied data file grants nothing.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Data is the on-disk document. Every field is a plain record.
type Data struct {
	Version int `json:"version"`

	Kubeconfigs []KubeconfigRecord `json:"kubeconfigs"`
	Clusters    []ClusterRecord    `json:"clusters"`
	Preferences []PreferenceRecord `json:"preferences"`

	// SeenContexts records the kubeconfig contexts auto-import has already
	// acted on. Without it, a cluster the engineer deliberately deleted would
	// reappear at the next launch.
	SeenContexts []SeenContextRecord `json:"seenContexts,omitempty"`

	// AutoImport is a pointer so that "never chosen" is distinguishable from
	// "switched off": a fresh install imports contexts by default, and an
	// explicit no survives a restart.
	AutoImport *bool `json:"autoImport,omitempty"`

	Appearance string `json:"appearance"`
}

// AutoImportEnabled reports whether new kubeconfig contexts should become
// clusters on their own.
func (d Data) AutoImportEnabled() bool { return d.AutoImport == nil || *d.AutoImport }

// CurrentVersion is the schema version written by this build.
const CurrentVersion = 1

// KubeconfigRecord remembers a kubeconfig the user imported.
//
// Path points at the user's own file. Biebie reads it; it does not rewrite it,
// because a kubeconfig is shared with kubectl, helm and every other tool the
// engineer relies on.
type KubeconfigRecord struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
	Path string `json:"path"`

	// Managed marks a copy Biebie owns inside its own data directory, which is
	// only created when the user explicitly asks for one.
	Managed bool `json:"managed"`

	ImportedAt string `json:"importedAt"`
}

// ClusterRecord is a stored cluster, serialised from the domain type.
type ClusterRecord struct {
	ID string `json:"id"`

	Name string `json:"name"`

	CustomerID   string `json:"customerId"`
	CustomerName string `json:"customerName"`

	EnvironmentID   string `json:"environmentId"`
	EnvironmentName string `json:"environmentName"`
	EnvironmentKind string `json:"environmentKind"`

	Server string `json:"server"`

	KubeconfigRef string `json:"kubeconfigRef"`
	ContextName   string `json:"contextName"`

	RequiresAccess  bool   `json:"requiresAccess"`
	AccessProfileID string `json:"accessProfileId,omitempty"`

	Labels map[string]string `json:"labels,omitempty"`

	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

// PreferenceRecord remembers per-cluster UI choices.
type PreferenceRecord struct {
	ClusterID     string `json:"clusterId"`
	LastNamespace string `json:"lastNamespace"`
}

// SeenContextRecord marks one kubeconfig context as already considered by
// auto-import, whether or not a cluster still exists for it.
type SeenContextRecord struct {
	KubeconfigRef string `json:"kubeconfigRef"`
	ContextName   string `json:"contextName"`
	SeenAt        string `json:"seenAt"`
}

// Store reads and writes the document under a mutex.
type Store struct {
	mu   sync.RWMutex
	path string
	data Data
}

// Open loads the document, creating an empty one on first run.
func Open(path string) (*Store, error) {
	s := &Store{path: path, data: Data{Version: CurrentVersion}}

	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return s, nil
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	if len(raw) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(raw, &s.data); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if s.data.Version > CurrentVersion {
		return nil, fmt.Errorf("%s was written by a newer Biebie Kube (version %d)", path, s.data.Version)
	}
	s.data.Version = CurrentVersion
	return s, nil
}

// Path reports where state is kept, shown in Settings.
func (s *Store) Path() string { return s.path }

// Read gives a caller a copy of the document.
func (s *Store) Read() Data {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.clone()
}

// Update applies a mutation and persists the result. The document is only
// replaced once the write has succeeded, so a failed save leaves memory and
// disk agreeing.
func (s *Store) Update(mutate func(*Data) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	next := s.data.clone()
	if err := mutate(&next); err != nil {
		return err
	}
	next.Version = CurrentVersion

	if err := s.write(next); err != nil {
		return err
	}
	s.data = next
	return nil
}

// write saves atomically: a crash mid-write must not leave a truncated file
// that loses every cluster the engineer configured.
func (s *Store) write(data Data) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}

	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".biebie-kube-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.Write(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("flush state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close state: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("secure state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}

func (d Data) clone() Data {
	out := d
	out.Kubeconfigs = append([]KubeconfigRecord(nil), d.Kubeconfigs...)
	out.Clusters = make([]ClusterRecord, len(d.Clusters))
	for i, c := range d.Clusters {
		out.Clusters[i] = c
		if c.Labels != nil {
			labels := make(map[string]string, len(c.Labels))
			for k, v := range c.Labels {
				labels[k] = v
			}
			out.Clusters[i].Labels = labels
		}
	}
	out.Preferences = append([]PreferenceRecord(nil), d.Preferences...)
	out.SeenContexts = append([]SeenContextRecord(nil), d.SeenContexts...)

	// The flag is copied through a new pointer so a caller mutating the clone
	// cannot reach into the document the store still holds.
	if d.AutoImport != nil {
		enabled := *d.AutoImport
		out.AutoImport = &enabled
	}
	return out
}

// DefaultPath returns the per-user location of the state document.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve configuration directory: %w", err)
	}
	return filepath.Join(dir, "biebie-kube", "data.json"), nil
}
