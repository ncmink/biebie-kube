package git

import (
	"fmt"
	"net"
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

// Transport is how git reaches a repository.
//
// Not a thing git needs told — it reads the URL itself — but a thing a person
// being asked why they cannot reach a repository needs told, because the
// answer to "what should I go and check" is a different answer for a key than
// for a token.
type Transport string

// The transports the allowlist admits.
const (
	TransportUnknown Transport = ""
	TransportSSH     Transport = "ssh"
	TransportHTTPS   Transport = "https"
	TransportHTTP    Transport = "http"
)

// Remote is a repository URL that is safe to hand to the git command.
//
// The parts beside the URL are filled in by ParseRemote, which has already
// taken the string apart to check it. Working them out a second time somewhere
// else would be a second implementation of the same parse, and the two would
// eventually disagree about something.
type Remote struct {
	url       string
	transport Transport
	host      string
	port      string

	// user is the account in the URL, kept only for ssh, where it is `git`
	// rather than a secret. An https userinfo never reaches this field: it is
	// stripped upstream and dropped by Display below in case it did not.
	user string
}

func (r Remote) String() string { return r.url }

// Transport says which of ssh or https git will use.
func (r Remote) Transport() Transport { return r.transport }

// Host is the machine git will talk to, without the port or the account.
func (r Remote) Host() string { return r.host }

// Address is host and port, ready to dial.
func (r Remote) Address() string {
	if r.host == "" {
		return ""
	}
	return net.JoinHostPort(r.host, r.portOrDefault())
}

func (r Remote) portOrDefault() string {
	switch {
	case r.port != "":
		return r.port
	case r.transport == TransportSSH:
		return "22"
	case r.transport == TransportHTTP:
		return "80"
	default:
		return "443"
	}
}

// SSHTarget is what ssh would be given to talk to this host on its own,
// `git@host`, and is empty for anything that is not ssh.
//
// It carries no repository. Asking a server who we are is a different question
// from asking it for a repository, and the whole reason to ask separately is
// to find out which of the two is failing.
func (r Remote) SSHTarget() string {
	if r.transport != TransportSSH || r.host == "" {
		return ""
	}
	user := r.user
	if user == "" {
		user = "git"
	}
	return user + "@" + r.host
}

// Display is the URL with anything credential-shaped taken out of it, for
// putting on a screen or in a command somebody will paste.
func (r Remote) Display() string { return Scrub(r.url) }

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
		scheme = strings.ToLower(scheme)
		if !schemes[scheme] {
			return Remote{}, unsupported(scheme)
		}
		parsed, err := url.Parse(value)
		if err != nil || parsed.Host == "" {
			return Remote{}, &Error{Kind: ErrorUnsupported, Message: "This repository URL cannot be read as a URL."}
		}
		remote := Remote{
			url:       value,
			transport: Transport(scheme),
			host:      parsed.Hostname(),
			port:      parsed.Port(),
		}
		if remote.transport == TransportSSH {
			// Only for ssh, where a username is an account name. The userinfo
			// on an https URL is frequently the token itself, and this struct
			// is not somewhere one needs to be kept.
			remote.user = parsed.User.Username()
		}
		return remote, nil
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
			user, machine, at := strings.Cut(host, "@")
			if !at {
				user, machine = "", host
			}
			// No port: scp-style syntax has nowhere to put one, which is why
			// a Host block in ~/.ssh/config is how people move these off 22.
			return Remote{url: value, transport: TransportSSH, host: machine, user: user}, nil
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
