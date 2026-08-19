// Package ipc carries Biebie Context Protocol messages between local
// applications, and only between local applications.
//
// The transport is a Unix domain socket on macOS and Linux and a named pipe on
// Windows. Nothing listens on a network interface — not 0.0.0.0, and not
// 127.0.0.1 — so a context cannot be requested from another machine, and no
// firewall prompt is ever raised.
//
// Access control is the operating system's: the socket directory is created
// 0700 and the socket 0600, so only the user who started the application can
// connect. Handoff redemption re-checks the OS user on top of that.
package ipc

import (
	bctx "biebie-kube/protocol/context"
)

// Endpoint names one application's local listening address.
type Endpoint struct {
	// Address is a socket path or a named-pipe name, depending on platform.
	Address string

	// Dir is the directory that must exist with restrictive permissions.
	// Empty on platforms where the transport has no filesystem presence.
	Dir string
}

// EndpointFor returns where the given application listens.
func EndpointFor(app bctx.App) (Endpoint, error) { return endpointFor(string(app)) }

// AccessEndpoint is where Biebie Access listens.
func AccessEndpoint() (Endpoint, error) { return EndpointFor(bctx.AppAccess) }

// KubeEndpoint is where Biebie Kube listens.
func KubeEndpoint() (Endpoint, error) { return EndpointFor(bctx.AppKube) }
