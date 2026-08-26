package update

import (
	"net"
	"net/http"
	"time"
)

// Timeouts for fetching a release. They bound the parts of an exchange that
// can hang without making progress, and deliberately leave the body read
// unbounded.
const (
	// dialTimeout bounds reaching the release host. GitHub's API and its
	// asset CDN are both public, so a dial that does not complete quickly is
	// a captive portal or a blocked network rather than a slow one.
	dialTimeout = 15 * time.Second

	// tlsHandshakeTimeout bounds the handshake for the same reason.
	tlsHandshakeTimeout = 15 * time.Second

	// responseHeaderTimeout bounds the wait for the first byte of the
	// response. Once headers arrive the transfer is underway and only its
	// size decides how long it takes.
	responseHeaderTimeout = 30 * time.Second
)

// NewHTTPClient returns the client the release provider downloads through.
//
// It exists because http.Client.Timeout bounds the whole exchange including
// reading the body, so the provider's own 30-second default cannot tell a dead
// host from an update that is simply large: an application zip is tens of
// megabytes, and any connection slower than a few megabits fails partway
// through with "context deadline exceeded (Client.Timeout or context
// cancellation while reading body)" after the progress bar has been climbing
// for half a minute.
//
// A download is therefore bounded by its context, which the updater window
// cancels when the user presses Cancel, and by transport timeouts that fire on
// a connection making no progress rather than on one that is merely long.
func NewHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{
		Timeout:   dialTimeout,
		KeepAlive: 30 * time.Second,
	}).DialContext
	transport.TLSHandshakeTimeout = tlsHandshakeTimeout
	transport.ResponseHeaderTimeout = responseHeaderTimeout

	return &http.Client{Transport: transport}
}
