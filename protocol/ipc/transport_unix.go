//go:build darwin || linux

package ipc

import (
	stdctx "context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// listen creates the socket with permissions that exclude every other OS user.
//
// A stale socket from a crashed process would block binding, so one that
// nothing answers on is removed first. A socket that still answers means the
// application is already running, and that is reported rather than stolen.
// maxUnixPath is the shortest sun_path limit across the platforms this
// transport supports (104 on macOS, 108 on Linux). A path past it fails with a
// bare "invalid argument" from bind, which is worth translating.
const maxUnixPath = 103

func listen(e Endpoint) (net.Listener, error) {
	if len(e.Address) > maxUnixPath {
		return nil, fmt.Errorf("socket path is %d bytes, over the %d-byte limit: %s",
			len(e.Address), maxUnixPath, e.Address)
	}
	if err := os.MkdirAll(e.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("create %s: %w", e.Dir, err)
	}
	if err := os.Chmod(e.Dir, 0o700); err != nil {
		return nil, fmt.Errorf("secure %s: %w", e.Dir, err)
	}

	if _, err := os.Stat(e.Address); err == nil {
		if alive(e.Address) {
			return nil, fmt.Errorf("another instance is already listening on %s", filepath.Base(e.Address))
		}
		if err := os.Remove(e.Address); err != nil {
			return nil, fmt.Errorf("remove stale socket: %w", err)
		}
	}

	ln, err := net.Listen("unix", e.Address)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", e.Address, err)
	}
	// The socket is tightened immediately after bind. The 0700 directory above
	// is what actually excludes other users: they cannot traverse into it even
	// during the moment before this chmod lands.
	if err := os.Chmod(e.Address, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("secure socket: %w", err)
	}
	return ln, nil
}

func dial(ctx stdctx.Context, e Endpoint, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "unix", e.Address)
}

// alive reports whether something currently accepts connections on the socket.
func alive(path string) bool {
	conn, err := net.DialTimeout("unix", path, 250*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
