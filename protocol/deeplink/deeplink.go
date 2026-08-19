// Package deeplink builds and parses the URLs Biebie applications use to open
// one another.
//
//	biebie-kube://open?handoff=hnd_ab23cd45ef67gh89
//	biebie-access://connect?profile=smoi-vpn&customer=smoi
//
// A deep link is visible to the window manager, the shell, and anything that
// logs process arguments. It therefore carries identifiers only. Parsing
// refuses a URL that contains credential-shaped parameters instead of quietly
// dropping them, so a mistake in the calling application is caught in review.
package deeplink

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	bctx "biebie-kube/protocol/context"
	"biebie-kube/protocol/handoff"
)

// URL schemes registered by each application.
const (
	SchemeKube   = "biebie-kube"
	SchemeAccess = "biebie-access"
)

// Hosts, the verb of the link.
const (
	HostOpen    = "open"
	HostConnect = "connect"
)

// Errors callers distinguish when a link cannot be honoured.
var (
	ErrScheme  = errors.New("not a Biebie deep link")
	ErrAction  = errors.New("unsupported deep-link action")
	ErrPayload = errors.New("deep link carries credential material")
)

// OpenKube builds the link Biebie Access uses to open a cluster in Biebie Kube.
func OpenKube(handoffID string) (string, error) {
	if !handoff.ValidID(handoffID) {
		return "", fmt.Errorf("invalid handoff id %q", handoffID)
	}
	u := url.URL{
		Scheme:   SchemeKube,
		Host:     HostOpen,
		RawQuery: url.Values{"handoff": {handoffID}}.Encode(),
	}
	return u.String(), nil
}

// ConnectAccess builds the link Biebie Kube uses to ask Biebie Access to bring
// a customer connection up. It names a profile; it never names a credential.
func ConnectAccess(profileID, customerID string) (string, error) {
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return "", errors.New("profile id is required")
	}
	query := url.Values{"profile": {profileID}}
	if customerID = strings.TrimSpace(customerID); customerID != "" {
		query.Set("customer", customerID)
	}
	u := url.URL{Scheme: SchemeAccess, Host: HostConnect, RawQuery: query.Encode()}
	return u.String(), nil
}

// OpenRequest is a parsed biebie-kube://open link.
type OpenRequest struct {
	HandoffID string
}

// ConnectRequest is a parsed biebie-access://connect link.
type ConnectRequest struct {
	ProfileID  string
	CustomerID string
}

// ParseOpen reads a link addressed to Biebie Kube.
func ParseOpen(raw string) (OpenRequest, error) {
	u, err := parse(raw, SchemeKube)
	if err != nil {
		return OpenRequest{}, err
	}
	if action(u) != HostOpen {
		return OpenRequest{}, fmt.Errorf("%w: %s", ErrAction, action(u))
	}
	id := u.Query().Get("handoff")
	if !handoff.ValidID(id) {
		return OpenRequest{}, fmt.Errorf("invalid handoff id in deep link")
	}
	return OpenRequest{HandoffID: id}, nil
}

// ParseConnect reads a link addressed to Biebie Access.
func ParseConnect(raw string) (ConnectRequest, error) {
	u, err := parse(raw, SchemeAccess)
	if err != nil {
		return ConnectRequest{}, err
	}
	if action(u) != HostConnect {
		return ConnectRequest{}, fmt.Errorf("%w: %s", ErrAction, action(u))
	}
	profile := strings.TrimSpace(u.Query().Get("profile"))
	if profile == "" {
		return ConnectRequest{}, errors.New("deep link is missing a profile")
	}
	return ConnectRequest{
		ProfileID:  profile,
		CustomerID: strings.TrimSpace(u.Query().Get("customer")),
	}, nil
}

// Scheme reports which application a link addresses, for a launcher that
// receives both.
func Scheme(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Scheme)
}

func parse(raw, want string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrScheme, err)
	}
	if !strings.EqualFold(u.Scheme, want) {
		return nil, fmt.Errorf("%w: scheme %q, expected %q", ErrScheme, u.Scheme, want)
	}
	if err := screen(u); err != nil {
		return nil, err
	}
	return u, nil
}

// action tolerates both biebie-kube://open?x=y and biebie-kube:///open?x=y,
// because different desktop launchers normalise the authority differently.
func action(u *url.URL) string {
	if host := strings.ToLower(u.Host); host != "" {
		return host
	}
	return strings.ToLower(strings.Trim(u.Path, "/"))
}

// screen refuses a link whose query looks like it carries a secret. Biebie
// never needs one in a URL, so its presence means something upstream is wrong.
func screen(u *url.URL) error {
	for key := range u.Query() {
		lower := strings.ToLower(key)
		for _, forbidden := range bctx.ForbiddenKeys() {
			if strings.Contains(lower, forbidden) {
				return fmt.Errorf("%w: parameter %q", ErrPayload, key)
			}
		}
	}
	return nil
}
