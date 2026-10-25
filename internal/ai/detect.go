package ai

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"

	"biebie-kube/internal/domain"
)

const probeTimeout = 5 * time.Second

const promptPlaceholder = "{{PROMPT}}"

// runner resolves and runs executables for detection tests.
type runner struct {
	look func(name string) (string, error)
	run  func(ctx context.Context, path string, args ...string) ([]byte, error)
}

func systemRunner() runner {
	return runner{
		look: exec.LookPath,
		run: func(ctx context.Context, path string, args ...string) ([]byte, error) {
			ctx, cancel := context.WithTimeout(ctx, probeTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, path, args...)
			var out bytes.Buffer
			cmd.Stdout = &out
			cmd.Stderr = &out
			err := cmd.Run()
			return out.Bytes(), err
		},
	}
}

// cliProfile is one built-in headless CLI mapping.
type cliProfile struct {
	id          string
	name        string
	command     string
	versionArgs []string
	// runArgs are appended after the resolved binary path; prompt goes on stdin.
	runArgs []string
}

var builtInProfiles = []cliProfile{
	{
		id:          "claude",
		name:        "Claude Code",
		command:     "claude",
		versionArgs: []string{"--version"},
		runArgs: []string{
			"-p",
			"--output-format", "text",
			"--permission-mode", "plan",
			"--tools", "",
			"--no-session-persistence",
		},
	},
	{
		id:          "codex",
		name:        "Codex CLI",
		command:     "codex",
		versionArgs: []string{"--version"},
		runArgs:     []string{"exec", "--sandbox", "read-only"},
	},
	{
		id:          "gemini",
		name:        "Gemini CLI",
		command:     "gemini",
		versionArgs: []string{"--version"},
		runArgs:     []string{},
	},
	{
		// Cursor ships the headless binary as `agent` (not `cursor`). It is
		// often installed to ~/.local/bin via the Cursor app.
		id:          "cursor",
		name:        "Cursor Agent",
		command:     "agent",
		versionArgs: []string{"--version"},
		// --trust accepts the empty temp working directory the runner creates
		// for every call; without it headless mode stops at a trust prompt.
		// --force/--yolo are deliberately not used: they allow every command.
		runArgs: []string{
			"--print",
			"--trust",
			"--mode", "ask",
			"--output-format", "text",
		},
	},
}

// Detector finds AI CLIs on the machine PATH.
type Detector struct {
	r runner
}

// NewDetector uses the real PATH.
func NewDetector() *Detector { return &Detector{r: systemRunner()} }

// ListBuiltIn probes each built-in profile.
func (d *Detector) ListBuiltIn(ctx context.Context) []domain.AICliInfo {
	out := make([]domain.AICliInfo, len(builtInProfiles))
	for i, p := range builtInProfiles {
		out[i] = d.probeBuiltIn(ctx, p)
	}
	return out
}

func (d *Detector) probeBuiltIn(ctx context.Context, p cliProfile) domain.AICliInfo {
	info := domain.AICliInfo{
		ID:      p.id,
		Name:    p.name,
		Command: p.command,
		RunArgs: append([]string{}, p.runArgs...),
	}
	path, err := d.r.look(p.command)
	if err != nil {
		info.Reason = p.name + " was not found on the PATH available to BiebieKube."
		return info
	}
	info.Path = path
	args := p.versionArgs
	if len(args) == 0 {
		args = []string{"--version"}
	}
	out, err := d.r.run(ctx, path, args...)
	if err != nil {
		if ctx.Err() != nil {
			info.Reason = p.name + " did not answer within " + probeTimeout.String() + "."
			return info
		}
		info.Reason = p.name + " was found but could not be run: " + firstLine(string(out))
		return info
	}
	info.Available = true
	info.Version = parseVersion(string(out))
	return info
}

// ProfileByID returns a built-in profile or an error.
func ProfileByID(id string) (cliProfile, error) {
	for _, p := range builtInProfiles {
		if p.id == id {
			return p, nil
		}
	}
	return cliProfile{}, errors.New("unknown AI CLI")
}

// ResolveInvocation turns a built-in or custom command into argv and whether
// the full message should be written to stdin.
func ResolveInvocation(cliID string, custom []domain.AICustomCommand, path string, prompt string) (argv []string, stdin string, err error) {
	if strings.HasPrefix(cliID, "custom:") {
		id := strings.TrimPrefix(cliID, "custom:")
		for _, c := range custom {
			if c.ID != id {
				continue
			}
			if strings.TrimSpace(c.Path) == "" {
				return nil, "", errors.New("custom command has no path")
			}
			return buildCustomArgv(c.Path, c.Args, prompt)
		}
		return nil, "", errors.New("custom command not found")
	}
	p, err := ProfileByID(cliID)
	if err != nil {
		return nil, "", err
	}
	if path == "" {
		return nil, "", errors.New("CLI is not available on this machine")
	}
	argv = append([]string{path}, p.runArgs...)
	return argv, prompt, nil
}

func buildCustomArgv(path string, args []string, prompt string) ([]string, string, error) {
	useStdin := false
	out := make([]string, 0, len(args)+1)
	out = append(out, path)
	for _, a := range args {
		if a == promptPlaceholder {
			useStdin = true
			continue
		}
		if strings.Contains(a, promptPlaceholder) {
			useStdin = true
			a = strings.ReplaceAll(a, promptPlaceholder, "")
		}
		if a != "" {
			out = append(out, a)
		}
	}
	if useStdin {
		return out, prompt, nil
	}
	// No placeholder: append prompt as final argument (still no shell).
	out = append(out, prompt)
	return out, "", nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

func parseVersion(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "v")
		if len(line) > 0 && line[0] >= '0' && line[0] <= '9' {
			return line
		}
	}
	return ""
}
