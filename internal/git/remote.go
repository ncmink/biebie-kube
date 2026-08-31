package git

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// The three values below are types rather than validated strings on purpose.
//
// All of them come off an object in a Kubernetes cluster: whoever may edit an
// Argo CD Application chooses the repository URL, the revision and the path.
// Git treats several of those strings as instructions — `ext::` names a
// transport that runs an arbitrary command, and a leading dash turns a URL
// into an option — so they are untrusted input in the strongest sense.
//
// Making them types means a value that has not been through the allowlist
// cannot reach a command line at all. The check is not something a later
// caller can forget to do, because there is no way to construct one without
// it.

// Remote is a repository URL that is safe to hand to the git command.
type Remote struct{ url string }

func (r Remote) String() string { return r.url }

// Revision is a branch, tag or commit that is safe to pass to git.
type Revision struct{ value string }

func (r Revision) String() string { return r.value }

// Path is a directory inside a repository.
type Path struct{ value string }

func (p Path) String() string { return p.value }

// IsRoot reports a path that names the whole repository.
func (p Path) IsRoot() bool { return p.value == "" }

// schemes the allowlist accepts.
//
// The list is short because it is the whole attack surface. `git://` is left
// out rather than forbidden for a reason: it is unauthenticated and almost
// never used for GitOps, and every transport kept off this list is one fewer
// thing a string from a cluster can ask for.
var schemes = map[string]bool{"https": true, "http": true, "ssh": true}

// ParseRemote checks a repository URL from an Argo CD Application.
func ParseRemote(raw string) (Remote, error) {
	value := strings.TrimSpace(raw)
	switch {
	case value == "":
		return Remote{}, &Error{Kind: ErrorUnsupported, Message: "This Application does not name a repository."}
	case strings.ContainsAny(value, "\n\r\x00"):
		return Remote{}, &Error{Kind: ErrorUnsupported, Message: "This repository URL contains a control character."}
	case strings.HasPrefix(value, "-"):
		// git would read it as an option rather than as a URL, which is how
		// `--upload-pack=…` becomes a command instead of an address.
		return Remote{}, &Error{Kind: ErrorUnsupported, Message: "This repository URL starts with a dash, so it will not be run."}
	}

	if scheme, _, found := strings.Cut(value, "://"); found {
		if !schemes[strings.ToLower(scheme)] {
			return Remote{}, unsupported(scheme)
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" {
			return Remote{}, &Error{Kind: ErrorUnsupported, Message: "This repository URL cannot be read as a URL."}
		}
		return Remote{url: value}, nil
	}

	// A transport helper is spelled `name::address` and never reaches the
	// scheme test above, because it has no `://` in it. `ext::` is the one
	// that matters: git runs the rest of the string as a command.
	if name, _, found := strings.Cut(value, "::"); found {
		return Remote{}, unsupported(name)
	}

	if host, repository, found := strings.Cut(value, ":"); found {
		// scp-style, `[user@]host:path`. A slash before the colon means the
		// colon is part of a path rather than a host separator, and a
		// single-letter host is a Windows drive.
		if !strings.Contains(host, "/") && repository != "" && len(host) > 1 && !strings.HasPrefix(repository, "\\") {
			return Remote{url: value}, nil
		}
	}
	return Remote{}, &Error{
		Kind:    ErrorUnsupported,
		Message: "This Application points at a path on a filesystem rather than at a repository over https or ssh.",
	}
}

func unsupported(transport string) *Error {
	return &Error{
		Kind:    ErrorUnsupported,
		Message: fmt.Sprintf("This Application uses the %q transport, which Biebie Kube does not run. Only https and ssh repositories are read.", transport),
	}
}

// ParseRevision checks a targetRevision from an Argo CD Application.
//
// The characters git allows in a ref name are broader than anything Argo CD
// writes, so the rule here is deliberately narrower. A value that would need
// `^`, `~` or `:` to be understood is revision syntax rather than a revision,
// and refusing it costs nothing while allowing it would let a string from a
// cluster address arbitrary objects in the repository.
func ParseRevision(raw string) (Revision, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		// Argo CD's own default when the field is left out.
		value = "HEAD"
	}
	if strings.HasPrefix(value, "-") {
		return Revision{}, &Error{Kind: ErrorUnsupported, Message: "A revision may not start with a dash."}
	}
	if strings.Contains(value, "..") {
		return Revision{}, &Error{Kind: ErrorUnsupported, Message: fmt.Sprintf("%q names a range rather than a revision.", raw)}
	}
	for _, r := range value {
		if !revisionRune(r) {
			return Revision{}, &Error{Kind: ErrorUnsupported, Message: fmt.Sprintf("%q is not a branch, tag or commit name.", raw)}
		}
	}
	return Revision{value: value}, nil
}

func revisionRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	}
	return strings.ContainsRune("._-/+", r)
}

// ParsePath checks the directory an Argo CD Application points at.
func ParsePath(raw string) (Path, error) {
	value := strings.TrimSpace(strings.ReplaceAll(raw, `\`, "/"))
	if value == "" || value == "." || value == "./" {
		return Path{}, nil
	}
	if strings.HasPrefix(value, "-") {
		return Path{}, &Error{Kind: ErrorUnsupported, Message: "A path may not start with a dash."}
	}
	if strings.HasPrefix(value, "/") {
		return Path{}, &Error{Kind: ErrorUnsupported, Message: fmt.Sprintf("%q is an absolute path rather than one inside the repository.", raw)}
	}
	cleaned := path.Clean(value)
	if cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return Path{}, &Error{Kind: ErrorUnsupported, Message: fmt.Sprintf("%q leads outside the repository.", raw)}
	}
	return Path{value: cleaned}, nil
}
