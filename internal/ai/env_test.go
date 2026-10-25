package ai

import "testing"

func TestDropEnv(t *testing.T) {
	cases := []struct {
		entry string
		drop  bool
	}{
		{"KUBECONFIG=/tmp/k", true},
		{"KUBERNETES_SERVICE_HOST=10.0.0.1", true},
		{"GITHUB_TOKEN=abc", true},
		{"DB_PASSWORD=secret", true},
		{"PATH=/usr/bin", false},
		{"HOME=/Users/me", false},
	}
	for _, c := range cases {
		if dropEnv(c.entry) != c.drop {
			t.Fatalf("%s: expected drop=%v", c.entry, c.drop)
		}
	}
}

func TestFilteredEnvironmentOmitsKube(t *testing.T) {
	t.Setenv("KUBECONFIG", "/secret")
	t.Setenv("PATH", "/bin")
	env := filteredEnvironment()
	for _, e := range env {
		if dropEnv(e) {
			t.Fatalf("leaked %s", e)
		}
	}
}
