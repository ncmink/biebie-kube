package domain

import (
	"testing"

	bctx "github.com/ncmink/biebie-protocol/context"
)

func TestClusterHostPortDefaultsToHTTPSPort(t *testing.T) {
	cases := map[string]string{
		"https://172.16.20.65:6443": "172.16.20.65:6443",
		"https://api.example.com":   "api.example.com:443",
		"172.16.20.65:6443":         "172.16.20.65:6443",
		"":                          "",
	}
	for server, want := range cases {
		c := Cluster{Server: server}
		if got := c.HostPort(); got != want {
			t.Fatalf("HostPort(%q) = %q, want %q", server, got, want)
		}
	}
}

func TestClusterTitleSkipsMissingParts(t *testing.T) {
	c := Cluster{CustomerName: "SMOI", EnvironmentName: "Production", Name: "RKE2 Production"}
	if got := c.Title(); got != "SMOI / Production / RKE2 Production" {
		t.Fatalf("title = %q", got)
	}
	bare := Cluster{Name: "docker-desktop"}
	if got := bare.Title(); got != "docker-desktop" {
		t.Fatalf("title = %q", got)
	}
}

func TestClusterInputRequiresProfileWhenAccessIsRequired(t *testing.T) {
	in := ClusterInput{
		Name:           "RKE2 Production",
		KubeconfigRef:  "kubeconfig_1",
		ContextName:    "default",
		RequiresAccess: true,
	}
	if err := in.Validate(); err == nil {
		t.Fatal("a cluster that needs customer access must name the profile that provides it")
	}
	in.AccessProfileID = "smoi-vpn"
	if err := in.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestClusterInputAllowsLocalClusterWithoutAccess(t *testing.T) {
	in := ClusterInput{Name: "docker-desktop", KubeconfigRef: "kubeconfig_1", ContextName: "docker-desktop"}
	if err := in.Validate(); err != nil {
		t.Fatalf("a local cluster must not require Biebie Access: %v", err)
	}
}

func TestProductionIsFlagged(t *testing.T) {
	c := Cluster{EnvironmentKind: bctx.EnvironmentProduction}
	if !c.IsProduction() {
		t.Fatal("production clusters must be marked")
	}
}

func TestLogOptionsClampTail(t *testing.T) {
	opts, err := LogOptions{Pod: "api", TailLines: 1 << 20}.Normalise()
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	if opts.TailLines != MaxTailLines {
		t.Fatalf("tail = %d, want %d", opts.TailLines, MaxTailLines)
	}

	defaulted, err := LogOptions{Pod: "api"}.Normalise()
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	if defaulted.TailLines != DefaultTailLines {
		t.Fatalf("tail = %d, want %d", defaulted.TailLines, DefaultTailLines)
	}
}

func TestPreviousLogsDoNotFollow(t *testing.T) {
	opts, err := LogOptions{Pod: "api", Previous: true, Follow: true}.Normalise()
	if err != nil {
		t.Fatalf("normalise: %v", err)
	}
	if opts.Follow {
		t.Fatal("a terminated container produces no new lines, so following it would hang")
	}
}

func TestPortForwardValidation(t *testing.T) {
	valid := PortForwardRequest{ResourceType: "pod", ResourceName: "grafana", RemotePort: 3000, LocalPort: 13000}
	if err := valid.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	for _, bad := range []PortForwardRequest{
		{ResourceType: "pod", RemotePort: 3000},
		{ResourceType: "deployment", ResourceName: "grafana", RemotePort: 3000},
		{ResourceType: "pod", ResourceName: "grafana", RemotePort: 0},
		{ResourceType: "pod", ResourceName: "grafana", RemotePort: 70000},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("%+v should not validate", bad)
		}
	}
}

func TestPortForwardURLIsLoopback(t *testing.T) {
	s := PortForwardSession{LocalPort: 13000}
	if got := s.URL(); got != "http://127.0.0.1:13000" {
		t.Fatalf("url = %q, a forward must never be published beyond loopback", got)
	}
}

func TestCatalogueIsAddressableAndSecretsAreSensitive(t *testing.T) {
	for _, info := range Catalogue() {
		if info.Resource == "" || info.Version == "" {
			t.Fatalf("%s has no group/version/resource mapping", info.Kind)
		}
		if _, ok := Lookup(info.Kind); !ok {
			t.Fatalf("%s is not addressable by lookup", info.Kind)
		}
	}
	secrets, _ := Lookup(KindSecret)
	if !secrets.Sensitive {
		t.Fatal("secrets must be marked sensitive so values stay masked by default")
	}
}
