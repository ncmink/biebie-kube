package kubeconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"biebie-kube/internal/store"
)

// Service owns the set of kubeconfigs Biebie Kube knows about.
type Service struct {
	store *store.Store

	// managedDir is where an explicitly copied kubeconfig is kept.
	managedDir string

	now func() time.Time
}

// NewService wires the service to persistent state.
func NewService(st *store.Store, managedDir string) *Service {
	return &Service{store: st, managedDir: managedDir, now: time.Now}
}

// List returns every imported kubeconfig with its contexts re-read from disk.
//
// Contexts are read live rather than cached: an engineer who adds a cluster
// with `kubectl config set-context` expects to see it here without an import
// step, and a file that has since been deleted must say so.
func (s *Service) List() []File {
	records := s.store.Read().Kubeconfigs
	out := make([]File, 0, len(records))
	for _, record := range records {
		file := File{
			Ref:     record.Ref,
			Name:    record.Name,
			Path:    record.Path,
			Managed: record.Managed,
		}
		if parsed, err := time.Parse(time.RFC3339, record.ImportedAt); err == nil {
			file.ImportedAt = parsed
		}
		contexts, err := Parse(record.Path)
		if err != nil {
			file.Error = err.Error()
		} else {
			file.Contexts = contexts
		}
		out = append(out, file)
	}
	return out
}

// Get returns one imported kubeconfig.
func (s *Service) Get(ref string) (File, error) {
	for _, file := range s.List() {
		if file.Ref == ref {
			return file, nil
		}
	}
	return File{}, fmt.Errorf("kubeconfig %s is not imported", ref)
}

// PathFor resolves a reference to a filesystem path for the client factory.
func (s *Service) PathFor(ref string) (string, error) {
	for _, record := range s.store.Read().Kubeconfigs {
		if record.Ref == ref {
			return record.Path, nil
		}
	}
	return "", fmt.Errorf("kubeconfig %s is not imported", ref)
}

// ImportOptions describes an import request from the UI.
type ImportOptions struct {
	Path string `json:"path"`
	Name string `json:"name"`

	// Copy asks Biebie to keep its own copy, for a file the user received
	// out-of-band and does not want to keep in ~/.kube.
	Copy bool `json:"copy"`
}

// Import indexes a kubeconfig after checking that it parses.
//
// A file that cannot be read is rejected at import time rather than at connect
// time, so the failure appears while the user is still looking at the dialog
// that caused it.
func (s *Service) Import(opts ImportOptions) (File, error) {
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return File{}, errors.New("a kubeconfig path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return File{}, fmt.Errorf("resolve %s: %w", path, err)
	}

	contexts, err := Parse(absolute)
	if err != nil {
		return File{}, err
	}

	if opts.Copy {
		absolute, err = s.copyIn(absolute)
		if err != nil {
			return File{}, err
		}
	}

	for _, record := range s.store.Read().Kubeconfigs {
		if record.Path == absolute {
			return File{}, fmt.Errorf("%s is already imported", filepath.Base(absolute))
		}
	}

	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = SuggestName(absolute)
	}

	record := store.KubeconfigRecord{
		Ref:        "kubeconfig_" + uuid.NewString(),
		Name:       name,
		Path:       absolute,
		Managed:    opts.Copy,
		ImportedAt: s.now().UTC().Format(time.RFC3339),
	}
	if err := s.store.Update(func(data *store.Data) error {
		data.Kubeconfigs = append(data.Kubeconfigs, record)
		return nil
	}); err != nil {
		return File{}, err
	}

	return File{
		Ref:        record.Ref,
		Name:       record.Name,
		Path:       record.Path,
		Managed:    record.Managed,
		ImportedAt: s.now(),
		Contexts:   contexts,
	}, nil
}

// ImportDefault indexes ~/.kube/config when it exists. It is called once on
// first run so the application is useful before the user configures anything.
func (s *Service) ImportDefault() (File, error) {
	path, err := DefaultPath()
	if err != nil {
		return File{}, err
	}
	if _, err := os.Stat(path); err != nil {
		return File{}, fmt.Errorf("no kubeconfig at %s", path)
	}
	return s.Import(ImportOptions{Path: path})
}

// Forget removes a reference. A managed copy is deleted with it; a file that
// belongs to the user is left exactly where it is.
func (s *Service) Forget(ref string) error {
	var removed *store.KubeconfigRecord
	err := s.store.Update(func(data *store.Data) error {
		kept := data.Kubeconfigs[:0]
		for _, record := range data.Kubeconfigs {
			if record.Ref == ref {
				copied := record
				removed = &copied
				continue
			}
			kept = append(kept, record)
		}
		if removed == nil {
			return fmt.Errorf("kubeconfig %s is not imported", ref)
		}
		data.Kubeconfigs = kept

		for _, cluster := range data.Clusters {
			if cluster.KubeconfigRef == ref {
				return fmt.Errorf("%s is still used by cluster %q", removed.Name, cluster.Name)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if removed.Managed {
		_ = os.Remove(removed.Path)
	}
	return nil
}

// copyIn places a copy inside Biebie's data directory with owner-only
// permissions, since a kubeconfig usually embeds a client certificate.
func (s *Service) copyIn(source string) (string, error) {
	if err := os.MkdirAll(s.managedDir, 0o700); err != nil {
		return "", fmt.Errorf("create managed directory: %w", err)
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", source, err)
	}
	target := filepath.Join(s.managedDir, uuid.NewString()+".yaml")
	if err := os.WriteFile(target, raw, 0o600); err != nil {
		return "", fmt.Errorf("write managed copy: %w", err)
	}
	return target, nil
}
