package deeplink

import (
	"errors"
	"testing"

	"biebie-kube/protocol/handoff"
)

func TestOpenKubeRoundTrip(t *testing.T) {
	id, err := handoff.NewID()
	if err != nil {
		t.Fatalf("id: %v", err)
	}
	link, err := OpenKube(id)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got, err := ParseOpen(link)
	if err != nil {
		t.Fatalf("parse %q: %v", link, err)
	}
	if got.HandoffID != id {
		t.Fatalf("handoff = %q, want %q", got.HandoffID, id)
	}
}

func TestParseOpenAcceptsSlashedAuthority(t *testing.T) {
	id, _ := handoff.NewID()
	if _, err := ParseOpen("biebie-kube:///open?handoff=" + id); err != nil {
		t.Fatalf("launchers that normalise the authority must still work: %v", err)
	}
}

func TestConnectAccessRoundTrip(t *testing.T) {
	link, err := ConnectAccess("smoi-vpn", "smoi")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	got, err := ParseConnect(link)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got.ProfileID != "smoi-vpn" || got.CustomerID != "smoi" {
		t.Fatalf("request = %+v", got)
	}
}

func TestParseRejectsForeignScheme(t *testing.T) {
	if _, err := ParseOpen("https://example.com/open?handoff=hnd_ab23cd45ef67gh89"); !errors.Is(err, ErrScheme) {
		t.Fatalf("error = %v, want ErrScheme", err)
	}
}

func TestParseRejectsCredentialParameters(t *testing.T) {
	id, _ := handoff.NewID()
	for _, link := range []string{
		"biebie-kube://open?handoff=" + id + "&password=secret123",
		"biebie-access://connect?profile=smoi-vpn&token=eyJhbGciOi",
		"biebie-access://connect?profile=smoi-vpn&kubeconfig=abc",
	} {
		_, openErr := ParseOpen(link)
		_, connectErr := ParseConnect(link)
		if !errors.Is(openErr, ErrPayload) && !errors.Is(connectErr, ErrPayload) {
			t.Fatalf("%q was accepted: open=%v connect=%v", link, openErr, connectErr)
		}
	}
}

func TestParseOpenRejectsMalformedHandoff(t *testing.T) {
	if _, err := ParseOpen("biebie-kube://open?handoff=nope"); err == nil {
		t.Fatal("a malformed handoff id must be rejected before any lookup")
	}
}
