package ai

import (
	"strings"
	"testing"
)

func TestResolveInvocationBuiltinNoShell(t *testing.T) {
	argv, stdin, err := ResolveInvocation("claude", nil, "/usr/bin/claude", "hello")
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) < 2 || argv[0] != "/usr/bin/claude" {
		t.Fatalf("unexpected argv: %v", argv)
	}
	if strings.Contains(strings.Join(argv, " "), "sh -c") {
		t.Fatal("must not use shell")
	}
	if stdin != "hello" {
		t.Fatalf("stdin: %q", stdin)
	}
}

func TestBuildCustomArgvPlaceholder(t *testing.T) {
	argv, stdin, err := buildCustomArgv("/opt/bin/ai", []string{"ask", promptPlaceholder}, "body")
	if err != nil {
		t.Fatal(err)
	}
	if stdin != "body" {
		t.Fatalf("stdin %q", stdin)
	}
	if len(argv) != 2 || argv[1] != "ask" {
		t.Fatalf("argv %v", argv)
	}
}
