// Package authoring writes Kubernetes objects that do not exist yet.
//
// Two things make this package different from internal/manifest, which edits
// objects that do. The first is that creation is never an update: the API
// server's refusal of a name already taken is the only thing standing between
// a new object and somebody else's, so every write here uses Create and none
// of them falls back to anything else.
//
// The second is that one of the two authoring surfaces runs code. cdk8s
// TypeScript is not a declarative format with an unusual syntax; it is a
// program, and synthesising it executes it through Node with the privileges of
// whoever is running Biebie Kube. That is a reasonable thing for a local
// DevOps tool to do and an unreasonable thing to be vague about, so it is
// stated here rather than discovered later: the TypeScript in the editor is
// trusted local user code, the same as a script they would run themselves.
//
// Nothing in this package goes through a shell. Every executable is found with
// exec.LookPath and invoked with an argument vector, so no value typed into
// the editor is ever expanded, quoted or split by anything but Go.
package authoring

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"biebie-kube/internal/domain"
)

// probeTimeout bounds asking a tool its version.
//
// Short on purpose. This runs behind a Settings panel a person is looking at,
// and an executable wedged on a network mount must not hold the panel open. A
// tool that cannot say its version within this is reported as unusable, which
// is what it is for the purpose of synthesising a manifest.
const probeTimeout = 5 * time.Second

// runner is how this package reaches the machine.
//
// It exists so detection can be tested without depending on what happens to be
// installed on whatever runs the tests. The two halves are separate because
// they fail differently: a tool that is not on the PATH and a tool that is
// there and broken are different sentences.
type runner struct {
	// look resolves a bare command name against PATH.
	look func(name string) (string, error)

	// run invokes an already-resolved executable with an argument vector and
	// returns its combined output. There is no variant that takes a command
	// line, which is the point: a shell cannot be reached from here.
	run func(ctx context.Context, dir, path string, args ...string) ([]byte, error)
}

func systemRunner() runner {
	return runner{look: exec.LookPath, run: execute}
}

// command builds the process this package would start.
//
// Separated from execute so a test can assert what would be run without
// running it. Everything is an argument: Path is the resolved executable and
// Args[0] is that same executable, never `sh`, never `-c`.
func command(ctx context.Context, dir, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	return cmd
}

// execute runs one process and returns everything it wrote.
//
// Stdout and stderr are read together because a tool that fails is usually
// explaining itself on the second one, and a person reading "cdk8s synth
// failed" with no output has been told nothing. Stdin is left nil so a program
// that decides to ask a question reads end-of-file and stops rather than
// waiting for somebody who is not there.
func execute(ctx context.Context, dir, path string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, synthTimeout)
	defer cancel()

	cmd := command(ctx, dir, path, args...)
	cmd.Env = environment()

	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	return out.Bytes(), err
}

// Detector reports what this machine can author with.
type Detector struct{ runner runner }

// NewDetector builds a detector against the real machine.
func NewDetector() *Detector { return &Detector{runner: systemRunner()} }

// tool is one executable and the argument that makes it say its version.
type tool struct {
	name    string
	args    []string
	purpose string
}

var typescriptTools = []tool{
	{name: "node", args: []string{"--version"}, purpose: "Node.js"},
	{name: "npm", args: []string{"--version"}, purpose: "npm"},
	{name: "cdk8s", args: []string{"--version"}, purpose: "cdk8s"},
}

// Detect looks for the TypeScript authoring runtime.
//
// Nothing is cached. Settings offers a Recheck button precisely because
// somebody installs cdk8s while the window is open, and a detector that
// remembered a failure would keep telling them it is missing.
func (d *Detector) Detect(ctx context.Context) domain.AuthoringRuntime {
	out := domain.AuthoringRuntime{YAML: true}

	statuses := make([]domain.ToolStatus, len(typescriptTools))
	for i, t := range typescriptTools {
		statuses[i] = d.probe(ctx, t)
	}
	out.Node, out.Npm, out.Cdk8s = statuses[0], statuses[1], statuses[2]

	var missing []string
	for i, status := range statuses {
		if !status.Available {
			missing = append(missing, typescriptTools[i].purpose)
		}
	}
	out.TypeScript = len(missing) == 0
	if !out.TypeScript {
		out.Reason = "TypeScript authoring needs " + join(missing) +
			". YAML authoring does not and stays available."
	}
	return out
}

// probe looks one tool up and asks it what it is.
//
// Being on the PATH is not enough to be usable: a Node left behind by a
// removed version manager is a symlink to nothing, and a wrapper that prints
// an upgrade notice and exits non-zero is worse than absent because it looks
// present. So the version is actually asked for, and a tool that will not
// answer is reported as unavailable with what it said instead.
func (d *Detector) probe(ctx context.Context, t tool) domain.ToolStatus {
	path, err := d.runner.look(t.name)
	if err != nil {
		return domain.ToolStatus{
			// Deliberately not "is not installed". A desktop application on
			// macOS is started by launchd with a PATH that has never seen
			// /opt/homebrew/bin, so the tool a person uses every day in their
			// terminal can be invisible here. Telling them to install what
			// they already have is the most irritating way to be wrong.
			Reason: t.purpose + " was not found on the PATH available to Biebie Kube.",
		}
	}

	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()

	out, err := d.runner.run(ctx, "", path, t.args...)
	if err != nil {
		// Both, because the two halves report a timeout differently: the
		// context knows the deadline passed, and exec reports the signal it
		// sent. Reading only the second turns a wedged tool into a broken one.
		if ctx.Err() != nil || errors.Is(err, context.DeadlineExceeded) {
			return domain.ToolStatus{
				Path:   path,
				Reason: t.purpose + " did not answer within " + probeTimeout.String() + " and was stopped.",
			}
		}
		return domain.ToolStatus{
			Path:   path,
			Reason: t.purpose + " was found but could not be run: " + firstLine(string(out)),
		}
	}

	// A version that cannot be read is not a reason to refuse the tool. cdk8s
	// prints an upgrade banner above its own version on some releases, and a
	// build from source prints something this parser has never seen. It ran,
	// so it counts; the field is simply left empty rather than filled with a
	// line of noise.
	return domain.ToolStatus{Available: true, Path: path, Version: version(string(out))}
}

// version pulls a version number out of what a tool printed.
//
// The first line that begins with a digit, once a leading `v` is dropped:
// node prints `v24.7.0`, npm prints `11.5.1`, and cdk8s sometimes prints a
// notice first. Anything else returns empty rather than a guess.
func version(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "v")
		if line == "" || line[0] < '0' || line[0] > '9' {
			continue
		}
		if field := strings.Fields(line)[0]; strings.Contains(field, ".") {
			return field
		}
	}
	return ""
}

func firstLine(out string) string {
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if len(line) > 200 {
				return line[:200] + "…"
			}
			return line
		}
	}
	return "it produced no output"
}

func join(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}
