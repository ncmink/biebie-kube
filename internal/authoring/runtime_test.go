package authoring

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// fakeMachine stands in for the machine's PATH and processes.
//
// Detection is tested against this rather than against whatever happens to be
// installed, because a CI runner with Node and a laptop without it would
// otherwise disagree about whether this code works.
type fakeMachine struct {
	found map[string]string
	says  map[string]string
	fails map[string]error

	// asked records every argument vector, so a test can assert that nothing
	// was handed to a shell.
	asked [][]string
}

func (m *fakeMachine) runner() runner {
	return runner{
		look: func(name string) (string, error) {
			path, ok := m.found[name]
			if !ok {
				return "", errors.New("executable file not found in $PATH")
			}
			return path, nil
		},
		run: func(ctx context.Context, dir, path string, args ...string) ([]byte, error) {
			m.asked = append(m.asked, append([]string{path}, args...))
			if err, ok := m.fails[path]; ok {
				return []byte(m.says[path]), err
			}
			return []byte(m.says[path]), nil
		},
	}
}

func ready() *fakeMachine {
	return &fakeMachine{
		found: map[string]string{
			"node":  "/opt/homebrew/bin/node",
			"npm":   "/opt/homebrew/bin/npm",
			"cdk8s": "/opt/homebrew/bin/cdk8s",
		},
		says: map[string]string{
			"/opt/homebrew/bin/node":  "v24.7.0\n",
			"/opt/homebrew/bin/npm":   "11.5.1\n",
			"/opt/homebrew/bin/cdk8s": "2.198.0\n",
		},
		fails: map[string]error{},
	}
}

func TestEveryToolFoundMakesTypeScriptAvailable(t *testing.T) {
	d := &Detector{runner: ready().runner()}

	out := d.Detect(context.Background())

	if !out.TypeScript {
		t.Fatalf("TypeScript unavailable with everything installed: %q", out.Reason)
	}
	if out.Node.Version != "24.7.0" || out.Npm.Version != "11.5.1" || out.Cdk8s.Version != "2.198.0" {
		t.Fatalf("versions = %q %q %q", out.Node.Version, out.Npm.Version, out.Cdk8s.Version)
	}
	if out.Node.Path != "/opt/homebrew/bin/node" {
		t.Fatalf("path = %q", out.Node.Path)
	}
}

func TestEachMissingToolIsReportedOnItsOwn(t *testing.T) {
	for _, missing := range []string{"node", "npm", "cdk8s"} {
		machine := ready()
		delete(machine.found, missing)

		out := (&Detector{runner: machine.runner()}).Detect(context.Background())

		if out.TypeScript {
			t.Fatalf("%s missing still reported TypeScript as ready", missing)
		}
		statuses := map[string]bool{
			"node": out.Node.Available, "npm": out.Npm.Available, "cdk8s": out.Cdk8s.Available,
		}
		for name, available := range statuses {
			if (name == missing) == available {
				t.Fatalf("with %s missing, %s.available = %v", missing, name, available)
			}
		}
	}
}

func TestAMissingToolDoesNotClaimItIsUninstalled(t *testing.T) {
	// A desktop application on macOS is launched by launchd with a PATH that
	// has never seen /opt/homebrew/bin. Telling somebody to install Node when
	// they use it every day in their terminal is the most irritating way to be
	// wrong, and it sends them to fix the wrong thing.
	machine := ready()
	delete(machine.found, "node")

	out := (&Detector{runner: machine.runner()}).Detect(context.Background())

	if !strings.Contains(out.Node.Reason, "PATH available to Biebie Kube") {
		t.Fatalf("reason does not name the PATH: %q", out.Node.Reason)
	}
	if strings.Contains(strings.ToLower(out.Node.Reason), "not installed") {
		t.Fatalf("reason claims the tool is not installed: %q", out.Node.Reason)
	}
}

func TestAToolThatWillNotAnswerIsUnavailable(t *testing.T) {
	// A Node left behind by a removed version manager is a symlink to nothing.
	// It is on the PATH, which is why looking it up is not enough.
	machine := ready()
	machine.fails["/opt/homebrew/bin/cdk8s"] = errors.New("exit status 127")
	machine.says["/opt/homebrew/bin/cdk8s"] = "dyld: Library not loaded\n"

	out := (&Detector{runner: machine.runner()}).Detect(context.Background())

	if out.Cdk8s.Available {
		t.Fatal("a tool that could not run was reported as available")
	}
	if !strings.Contains(out.Cdk8s.Reason, "Library not loaded") {
		t.Fatalf("reason does not say what happened: %q", out.Cdk8s.Reason)
	}
	if out.Cdk8s.Path == "" {
		t.Fatal("the path that failed was not reported, so nobody can go and look at it")
	}
}

func TestAToolThatHangsIsStoppedRatherThanWaitedFor(t *testing.T) {
	machine := ready()
	// What the real runner produces when the probe deadline passes: exec kills
	// the process, and the context is what says why.
	machine.fails["/opt/homebrew/bin/npm"] = context.DeadlineExceeded

	out := (&Detector{runner: machine.runner()}).Detect(context.Background())

	if out.Npm.Available {
		t.Fatal("a tool that never answered was reported as available")
	}
	if !strings.Contains(out.Npm.Reason, "did not answer") {
		t.Fatalf("reason does not say it timed out: %q", out.Npm.Reason)
	}
}

func TestAVersionNobodyCanParseDoesNotDisqualifyTheTool(t *testing.T) {
	// cdk8s prints an upgrade notice above its version on some releases, and a
	// build from source prints something this parser has never seen. It ran,
	// so it counts.
	machine := ready()
	machine.says["/opt/homebrew/bin/cdk8s"] = "cdk8s-cli built from source\n"

	out := (&Detector{runner: machine.runner()}).Detect(context.Background())

	if !out.Cdk8s.Available {
		t.Fatalf("an unparseable version made the tool unavailable: %q", out.Cdk8s.Reason)
	}
	if out.Cdk8s.Version != "" {
		t.Fatalf("a version was invented from unparseable output: %q", out.Cdk8s.Version)
	}
}

func TestAVersionIsReadPastANotice(t *testing.T) {
	machine := ready()
	machine.says["/opt/homebrew/bin/cdk8s"] = "\nA newer version of cdk8s is available.\n2.198.0\n"

	out := (&Detector{runner: machine.runner()}).Detect(context.Background())

	if out.Cdk8s.Version != "2.198.0" {
		t.Fatalf("version = %q", out.Cdk8s.Version)
	}
}

func TestYAMLAuthoringSurvivesAMissingTypeScriptRuntime(t *testing.T) {
	// This is the product rule the whole runtime section exists to protect:
	// somebody with no Node installed can still create a ConfigMap.
	machine := &fakeMachine{found: map[string]string{}, says: map[string]string{}, fails: map[string]error{}}

	out := (&Detector{runner: machine.runner()}).Detect(context.Background())

	if out.TypeScript {
		t.Fatal("TypeScript reported ready with nothing installed")
	}
	if !out.YAML {
		t.Fatal("YAML authoring was withdrawn because Node is missing")
	}
	if !strings.Contains(out.Reason, "YAML authoring does not") {
		t.Fatalf("the reason does not say YAML still works: %q", out.Reason)
	}
}

func TestTheReasonNamesEveryMissingTool(t *testing.T) {
	machine := ready()
	delete(machine.found, "npm")
	delete(machine.found, "cdk8s")

	out := (&Detector{runner: machine.runner()}).Detect(context.Background())

	if !strings.Contains(out.Reason, "npm and cdk8s") {
		t.Fatalf("reason = %q", out.Reason)
	}
}

func TestNoDetectionGoesThroughAShell(t *testing.T) {
	// The security boundary of this package. A command line assembled into a
	// string and handed to `sh -c` is a command line whose quoting is somebody
	// else's problem; an argument vector has no quoting at all.
	machine := ready()

	(&Detector{runner: machine.runner()}).Detect(context.Background())

	if len(machine.asked) != 3 {
		t.Fatalf("asked %d tools, expected 3", len(machine.asked))
	}
	for _, invocation := range machine.asked {
		for _, argument := range invocation {
			if argument == "-c" || strings.HasSuffix(argument, "/sh") || strings.HasSuffix(argument, "/bash") {
				t.Fatalf("a shell was involved: %v", invocation)
			}
		}
	}
}

func TestTheProcessThisPackageBuildsIsTheExecutableItself(t *testing.T) {
	// The runner above is a fake, so it cannot prove what the real one does.
	// This checks the one function that actually constructs a process.
	cmd := command(context.Background(), "/tmp", "/opt/homebrew/bin/cdk8s", "synth")

	if cmd.Path != "/opt/homebrew/bin/cdk8s" {
		t.Fatalf("path = %q", cmd.Path)
	}
	if len(cmd.Args) != 2 || cmd.Args[0] != "/opt/homebrew/bin/cdk8s" || cmd.Args[1] != "synth" {
		t.Fatalf("args = %v", cmd.Args)
	}
	if cmd.Dir != "/tmp" {
		t.Fatalf("the working directory was not set on the process: %q", cmd.Dir)
	}
	var _ *exec.Cmd = cmd
}

func TestTheChildEnvironmentDoesNotCarryEverythingThisProcessHolds(t *testing.T) {
	// A synth failure is shown to the user, so what the child can print is
	// what the child was given. Passing os.Environ() would mean a kubeconfig
	// path, an access token an exec plugin was handed, and whatever the
	// person's shell exports, all one stack trace away.
	t.Setenv("BIEBIE_KUBE_SECRET_FOR_TEST", "hunter2")

	for _, entry := range environment() {
		if strings.Contains(entry, "hunter2") {
			t.Fatalf("an unrelated variable reached the child: %q", entry)
		}
	}
}

func TestProxyCredentialsAreScrubbedFromToolOutput(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://someone:hunter2@proxy.internal:3128")

	out := scrub("npm ERR! request to http://someone:hunter2@proxy.internal:3128 failed")

	if strings.Contains(out, "hunter2") {
		t.Fatalf("a proxy credential survived scrubbing: %q", out)
	}
}
