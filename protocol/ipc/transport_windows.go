//go:build windows

package ipc

import (
	stdctx "context"
	"fmt"
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// pipeSecurity is a security descriptor granting access to the creating user
// and to local SYSTEM, and to nobody else. In SDDL:
//
//	D:      discretionary ACL
//	(A;;GA;;;OW)  allow generic-all to the owner
//	(A;;GA;;;SY)  allow generic-all to SYSTEM
//
// Without this a named pipe is readable by every interactive user on the
// machine, which is exactly what this transport must not permit.
const pipeSecurity = "D:(A;;GA;;;OW)(A;;GA;;;SY)"

func listen(e Endpoint) (net.Listener, error) {
	ln, err := winio.ListenPipe(e.Address, &winio.PipeConfig{
		SecurityDescriptor: pipeSecurity,
		MessageMode:        false,
	})
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", e.Address, err)
	}
	return ln, nil
}

func dial(ctx stdctx.Context, e Endpoint, timeout time.Duration) (net.Conn, error) {
	dialCtx, cancel := stdctx.WithTimeout(ctx, timeout)
	defer cancel()
	return winio.DialPipeContext(dialCtx, e.Address)
}
