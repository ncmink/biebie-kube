//go:build windows

package ipc

// endpointFor uses a named pipe, which has no filesystem presence to permit.
// The pipe's own security descriptor restricts it to the creating user.
func endpointFor(app string) (Endpoint, error) {
	return Endpoint{Address: `\\.\pipe\` + app}, nil
}
