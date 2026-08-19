// Package kubeconfig indexes the kubeconfig files an engineer already has.
//
// The rule this package exists to enforce: a kubeconfig belongs to the user,
// not to Biebie. kubectl, helm, k9s and CI scripts all read the same file, so
// importing one here means reading and referencing it — never rewriting it,
// never reordering contexts, and never changing current-context behind the
// user's back. A Biebie-managed copy is created only when explicitly asked for.
package kubeconfig

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// ContextEntry is one context found inside a kubeconfig file.
type ContextEntry struct {
	Name      string `json:"name"`
	Cluster   string `json:"cluster"`
	Server    string `json:"server"`
	User      string `json:"user"`
	Namespace string `json:"namespace,omitempty"`

	// Current marks the file's own current-context, used only as a sensible
	// default in the import dialog.
	Current bool `json:"current"`
}

// File is an indexed kubeconfig.
type File struct {
	Ref  string `json:"ref"`
	Name string `json:"name"`
	Path string `json:"path"`

	Managed bool `json:"managed"`

	ImportedAt time.Time `json:"importedAt"`

	Contexts []ContextEntry `json:"contexts"`

	// Error is set when a previously imported file can no longer be read, so
	// the UI can show why its clusters stopped working instead of hiding them.
	Error string `json:"error,omitempty"`
}

// ErrNoContexts means the file parsed but describes nothing to connect to.
var ErrNoContexts = errors.New("this kubeconfig contains no contexts")

// DefaultPath is the conventional location of the user's kubeconfig.
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".kube", "config"), nil
}

// Parse reads a kubeconfig and lists what it can connect to.
//
// The file is opened read-only and closed immediately; nothing is written back
// under any circumstances.
func Parse(path string) ([]ContextEntry, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("a kubeconfig path is required")
	}
	config, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return nil, fmt.Errorf("read kubeconfig: %w", err)
	}
	entries := contextsOf(config)
	if len(entries) == 0 {
		return nil, ErrNoContexts
	}
	return entries, nil
}

func contextsOf(config *clientcmdapi.Config) []ContextEntry {
	entries := make([]ContextEntry, 0, len(config.Contexts))
	for name, kctx := range config.Contexts {
		entry := ContextEntry{
			Name:      name,
			Cluster:   kctx.Cluster,
			User:      kctx.AuthInfo,
			Namespace: kctx.Namespace,
			Current:   name == config.CurrentContext,
		}
		if cluster, ok := config.Clusters[kctx.Cluster]; ok {
			entry.Server = cluster.Server
		}
		entries = append(entries, entry)
	}
	// Map iteration is random, and a list that reorders itself between reads
	// makes the import dialog feel broken.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })
	return entries
}

// ServerFor returns the API endpoint a context points at, used to fill in a
// cluster record at import time.
func ServerFor(path, contextName string) (string, error) {
	entries, err := Parse(path)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Name == contextName {
			return entry.Server, nil
		}
	}
	return "", fmt.Errorf("context %q is not in %s", contextName, filepath.Base(path))
}

// SuggestName derives a display name for an imported file.
func SuggestName(path string) string {
	base := filepath.Base(path)
	if base == "config" {
		if parent := filepath.Base(filepath.Dir(path)); parent == ".kube" {
			return "Default kubeconfig"
		}
	}
	return base
}
