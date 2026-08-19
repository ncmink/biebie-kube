//go:build darwin

package ipc

import (
	"fmt"
	"os"
	"path/filepath"
)

// endpointFor places sockets beside the applications' own data, under a shared
// Biebie directory.
//
// A Unix socket path is limited to about 104 bytes on macOS, which this
// location comfortably fits.
func endpointFor(app string) (Endpoint, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Endpoint{}, fmt.Errorf("resolve home directory: %w", err)
	}
	dir := filepath.Join(home, "Library", "Application Support", "Biebie")
	return Endpoint{Dir: dir, Address: filepath.Join(dir, app+".sock")}, nil
}
