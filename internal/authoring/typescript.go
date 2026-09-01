package authoring

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"biebie-kube/internal/domain"
)

// synthTimeout bounds one cdk8s invocation.
//
// Generous compared with the version probe, because this one is compiling and
// running a program. Bounded all the same: the TypeScript in the editor is
// trusted local code, and trusted local code still contains infinite loops.
const synthTimeout = 90 * time.Second

// installTimeout bounds the one-off dependency install.
const installTimeout = 10 * time.Minute

// environment is what a child process is allowed to see.
//
// A reduced set rather than os.Environ(). Everything this application holds is
// in that environment — a kubeconfig path, an access token an exec plugin was
// given, whatever the person's shell exports — and none of it is any business
// of a manifest generator. What is left is what npm and Node genuinely need to
// work on a corporate machine.
//
// This is also why a synth failure can be shown to the user at all: the output
// cannot contain a variable that was never passed.
func environment() []string {
	out := []string{
		// npm and Node both shell out to nothing, but Node resolves its own
		// helpers through PATH and npm looks for git there.
		"PATH=" + os.Getenv("PATH"),

		// npm keeps its cache and configuration under the home directory.
		// Without it every install starts from an empty cache.
		"HOME=" + os.Getenv("HOME"),

		// A registry that is only reachable through a proxy is an ordinary
		// customer environment.
		"HTTP_PROXY=" + os.Getenv("HTTP_PROXY"),
		"HTTPS_PROXY=" + os.Getenv("HTTPS_PROXY"),
		"NO_PROXY=" + os.Getenv("NO_PROXY"),

		// No terminal to draw a progress bar on and nobody to answer a
		// prompt, so ask for neither.
		"CI=1",
		"npm_config_fund=false",
		"npm_config_audit=false",
		"npm_config_progress=false",
	}
	if tmp := os.Getenv("TMPDIR"); tmp != "" {
		out = append(out, "TMPDIR="+tmp)
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		out = append(out, "APPDATA="+appData)
	}
	if userProfile := os.Getenv("USERPROFILE"); userProfile != "" {
		out = append(out, "USERPROFILE="+userProfile)
	}
	return out
}

// scrub removes from a tool's output the few things this application handed it.
//
// The environment is already reduced, so the only secret that can reach a
// child is a credential embedded in a proxy URL — which npm prints back on a
// connection failure. The home directory is shortened for the same reason it
// is everywhere else in this application: a screenshot of an error should not
// carry somebody's username.
func scrub(out string) string {
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" || !strings.Contains(value, "@") {
			continue
		}
		out = strings.ReplaceAll(out, value, "[proxy]")
		// npm also prints the userinfo on its own in some messages.
		if before, _, found := strings.Cut(value, "@"); found {
			if _, credentials, ok := strings.Cut(before, "//"); ok && credentials != "" {
				out = strings.ReplaceAll(out, credentials, "[redacted]")
			}
		}
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" && home != "/" {
		out = strings.ReplaceAll(out, home, "~")
	}
	return out
}

// Prepare installs the fixed dependency set, once.
//
// This is the only place in the application that runs npm install, it runs
// against a package.json this application wrote, and it is reached by a button
// in Settings rather than by opening an editor. That ordering is the point:
// pressing Preview must never be the thing that starts a network install.
func (s *Service) Prepare(ctx context.Context) (domain.AuthoringRuntime, error) {
	runtime := s.Runtime(ctx)
	if !runtime.TypeScript {
		return runtime, errors.New(runtime.Reason)
	}
	if err := s.workspace.ensureRuntime(); err != nil {
		return runtime, err
	}

	ctx, cancel := context.WithTimeout(ctx, installTimeout)
	defer cancel()

	// `npm install` rather than `npm ci`: there is no lockfile to honour on a
	// first run, and the package.json above is the only input.
	out, err := s.runner.run(ctx, s.workspace.runtimeDir(), runtime.Npm.Path, "install", "--no-fund", "--no-audit")
	if err != nil {
		return s.Runtime(ctx), fmt.Errorf("installing the cdk8s dependencies failed: %s", firstLine(scrub(string(out))))
	}
	return s.Runtime(ctx), nil
}

// Synthesize turns the TypeScript in one session into a manifest.
//
// The generated YAML is what everything downstream sees. The TypeScript is
// never validated, never previewed and never applied — it is a program that
// produced a manifest, and the manifest is the thing with a meaning in
// Kubernetes.
func (s *Service) Synthesize(ctx context.Context, sessionID, source string) (string, string, error) {
	runtime := s.Runtime(ctx)
	if !runtime.TypeScript {
		return "", "", errors.New(runtime.Reason)
	}
	if !runtime.Prepared {
		return "", "", errors.New("the cdk8s dependencies have not been installed yet. Settings → Resource Authoring installs them once.")
	}

	session, err := s.workspace.Session(sessionID)
	if err != nil {
		return "", "", err
	}

	if err := os.WriteFile(filepath.Join(session.dir, "main.ts"), []byte(source), 0o644); err != nil {
		return "", "", fmt.Errorf("write the TypeScript: %w", err)
	}
	// A previous synth's output would otherwise be read back as this one's if
	// cdk8s failed before writing anything, which is the failure mode that
	// shows somebody a manifest they did not generate.
	if err := os.RemoveAll(filepath.Join(session.dir, "dist")); err != nil {
		return "", "", fmt.Errorf("clear the previous manifest: %w", err)
	}

	out, err := s.runner.run(ctx, session.dir, runtime.Cdk8s.Path, "synth")
	output := scrub(string(out))
	if err != nil {
		return "", output, fmt.Errorf("cdk8s could not synthesise this TypeScript: %s", firstLine(errorLines(output)))
	}

	manifest, err := readDist(filepath.Join(session.dir, "dist"))
	if err != nil {
		return "", output, err
	}
	return manifest, output, nil
}

// readDist collects everything cdk8s wrote.
//
// A chart per file, in name order, joined as one multi-document manifest. The
// order is fixed rather than whatever the filesystem returns, so the preview a
// person reads is the order the objects are created in.
func readDist(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", errors.New("cdk8s finished but wrote no manifest")
	}

	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if name := entry.Name(); strings.HasSuffix(name, ".yaml") || strings.HasSuffix(name, ".yml") {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return "", errors.New("cdk8s finished but wrote no manifest")
	}
	sort.Strings(names)

	var parts []string
	for _, name := range names {
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return "", fmt.Errorf("read the generated manifest: %w", err)
		}
		if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	if len(parts) == 0 {
		return "", errors.New("cdk8s wrote an empty manifest")
	}
	return strings.Join(parts, "\n---\n") + "\n", nil
}

// errorLines picks the part of cdk8s output worth leading with.
//
// A TypeScript error arrives as `main.ts(12,3): error TS2551: …` among a page
// of ts-node framing, and a person shown the first line of that page has been
// told that cdk8s ran.
func errorLines(out string) string {
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "error ts") || strings.Contains(lower, "error:") ||
			strings.HasPrefix(lower, "typeerror") || strings.HasPrefix(lower, "referenceerror") ||
			strings.HasPrefix(lower, "syntaxerror") {
			kept = append(kept, trimmed)
		}
	}
	if len(kept) == 0 {
		return out
	}
	return strings.Join(kept, "\n")
}
