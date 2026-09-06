package cluster

import (
	"context"
	"strings"
	"testing"

	bctx "github.com/ncmink/biebie-protocol/context"

	"biebie-kube/internal/domain"
)

func tunnelled() domain.Cluster {
	return domain.Cluster{Server: "https://172.16.20.65:6443"}
}

func apiForward() bctx.Forward {
	return bctx.Forward{Name: "Kubernetes API", LocalPort: 16443, RemoteHost: "localhost", RemotePort: 6443}
}

// Without the tunnel, the cluster is dialled and verified exactly as its
// kubeconfig says. Nothing about this path may change for the clusters that
// were reachable all along.
func TestWithoutATunnelTheClusterIsDialledAsWritten(t *testing.T) {
	address, via := accessCheck{required: true, confirmed: true}.endpointFor(tunnelled())

	if address != "172.16.20.65:6443" {
		t.Fatalf("address = %q, want the cluster's own", address)
	}
	if via != nil {
		t.Fatalf("endpoint override = %+v, want none", via)
	}
}

func TestATunnelRedirectsTheDialButNotTheVerification(t *testing.T) {
	address, via := accessCheck{
		required: true, confirmed: true, viaTunnel: true, forward: apiForward(),
	}.endpointFor(tunnelled())

	if address != "127.0.0.1:16443" {
		t.Fatalf("address = %q, want the local stand-in", address)
	}
	if via == nil {
		t.Fatal("a tunnelled cluster needs an endpoint override")
	}
	if via.Address != "127.0.0.1:16443" {
		t.Errorf("override address = %q", via.Address)
	}
	// The API server's certificate is issued for the cluster's own address.
	// Dropping this is what would push someone towards insecure-skip-tls-verify.
	if via.ServerName != "172.16.20.65" {
		t.Errorf("server name = %q, want the cluster's own host", via.ServerName)
	}
}

// A bare loopback address in a cluster's probe list reads as a bug — the
// engineer knows their cluster is not on this machine.
func TestTheProbeSaysWhoseLoopbackPortThatIs(t *testing.T) {
	cluster := tunnelled()

	plain := accessCheck{required: true, confirmed: true}.describe(cluster)
	if plain != "172.16.20.65:6443" {
		t.Fatalf("described an ordinary cluster as %q", plain)
	}

	described := accessCheck{
		required: true, confirmed: true, viaTunnel: true, forward: apiForward(),
	}.describe(cluster)

	for _, want := range []string{"127.0.0.1:16443", "Biebie Access", "172.16.20.65:6443"} {
		if !strings.Contains(described, want) {
			t.Errorf("description %q does not mention %q", described, want)
		}
	}
}

func TestOtherForwardsExcludesTheAPIServer(t *testing.T) {
	all := []bctx.Forward{
		apiForward(),
		{LocalPort: 31538, RemoteHost: "localhost", RemotePort: 31539},
	}

	extras := otherForwards(all, apiForward(), true)
	if len(extras) != 1 || extras[0].LocalPort != 31538 {
		t.Fatalf("extras = %+v, want only the non-Kubernetes forward", extras)
	}

	// A connection whose forwards are all for other things still has them to
	// offer, even though none of them is this cluster.
	extras = otherForwards(all, bctx.Forward{}, false)
	if len(extras) != 2 {
		t.Fatalf("extras = %+v, want every forward when none is the API server", extras)
	}
}

// lender is a Biebie Access that is up and lending ports.
type lender struct{ status bctx.AccessStatus }

func (l lender) Installed(context.Context) bool { return true }

func (l lender) Status(context.Context, string) (bctx.AccessStatus, error) {
	return l.status, nil
}

func (l lender) Connect(context.Context, string) (string, error) { return "", nil }

// checkAccess is where a published port map becomes a decision about how to
// reach this particular cluster, so it is worth testing as one step rather than
// only through its parts.
func TestCheckAccessSplitsTheAPIServerFromTheRest(t *testing.T) {
	cluster := tunnelled()
	cluster.Access = domain.AccessRequirement{Required: true, ProfileID: "p1"}

	manager := &Manager{access: lender{status: bctx.AccessStatus{
		Connected: true,
		Gateway:   "172.16.20.65",
		Forwards: []bctx.Forward{
			apiForward(),
			{Name: "Argo CD", LocalPort: 31538, RemoteHost: "localhost", RemotePort: 31539},
		},
	}}}

	probes := &builder{}
	check := manager.checkAccess(context.Background(), cluster, probes)

	if !check.confirmed || !check.viaTunnel {
		t.Fatalf("check = %+v, want a confirmed tunnelled connection", check)
	}
	if check.forward.LocalPort != 16443 {
		t.Errorf("API forward = %+v, want the one matching the cluster", check.forward)
	}
	if len(check.extras) != 1 || check.extras[0].LocalPort != 31538 {
		t.Errorf("extras = %+v, want only the non-cluster forward", check.extras)
	}
	// Without the gateway, the page cannot say whose localhost the far end of a
	// forward is, and both ends read as this machine.
	if check.gateway != "172.16.20.65" {
		t.Errorf("gateway = %q, want the machine the ports come from", check.gateway)
	}

	// The engineer reading the diagnosis panel has to be told the API server was
	// reached through a tunnel, or a loopback address there looks like a bug.
	if len(probes.probes) != 1 {
		t.Fatalf("probes = %+v, want one access probe", probes.probes)
	}
	if !strings.Contains(probes.probes[0].Detail, "127.0.0.1:16443") {
		t.Errorf("access probe says %q, without the address actually used", probes.probes[0].Detail)
	}
}

// A VPN hands this machine a route, so there is nothing to substitute and the
// cluster must still be dialled at its own address.
func TestCheckAccessOverAVPNLeavesTheAddressAlone(t *testing.T) {
	cluster := tunnelled()
	cluster.Access = domain.AccessRequirement{Required: true, ProfileID: "p1"}

	manager := &Manager{access: lender{status: bctx.AccessStatus{
		Connected:  true,
		AssignedIP: "10.42.0.7",
	}}}

	check := manager.checkAccess(context.Background(), cluster, &builder{})
	if !check.confirmed {
		t.Fatalf("check = %+v, want confirmed", check)
	}
	if check.viaTunnel {
		t.Error("a VPN was mistaken for a tunnel")
	}
	if address, via := check.endpointFor(cluster); address != "172.16.20.65:6443" || via != nil {
		t.Errorf("endpoint = %q via %+v, want the cluster's own", address, via)
	}
}

// The port is half of the match against a published forward, and a kubeconfig
// that omits it means the HTTPS port, not "no port".
func TestClusterPortMatchesHostPort(t *testing.T) {
	cases := []struct {
		server string
		port   int
	}{
		{"https://172.16.20.65:6443", 6443},
		{"https://kube.example", 443},
		{"172.16.20.65:6443", 6443},
		{"", 0},
	}

	for _, tc := range cases {
		t.Run(tc.server, func(t *testing.T) {
			if got := (domain.Cluster{Server: tc.server}).Port(); got != tc.port {
				t.Fatalf("port = %d, want %d", got, tc.port)
			}
		})
	}
}
