//go:build linux

package ipc

import (
	"fmt"
	"os"
	"path/filepath"
)

// endpointFor prefers the per-user runtime directory, which the system clears
// at logout. When it is not set — a bare container, or a minimal session — it
// falls back to a dot directory in the user's home.
func endpointFor(app string) (Endpoint, error) {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return Endpoint{}, fmt.Errorf("resolve home directory: %w", err)
		}
		base = filepath.Join(home, ".biebie")
	}
	dir := filepath.Join(base, "biebie")
	return Endpoint{Dir: dir, Address: filepath.Join(dir, app+".sock")}, nil
}
