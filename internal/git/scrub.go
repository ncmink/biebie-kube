package git

import "regexp"

// Anything of the form `scheme://something@host`. The userinfo is what is
// being looked for; the host and the rest of the URL are worth keeping,
// because "could not reach github.com" is the useful half of the sentence.
var (
	webUserinfo   = regexp.MustCompile(`(https?://)[^/@\s]*@`)
	otherPassword = regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9+.\-]*://[^/:@\s]*):[^/@\s]*@`)
)

// Scrub removes credentials from text git wrote.
//
// git quotes the URL it was given back in its own errors — `fatal:
// Authentication failed for 'https://user:ghp_token@github.com/acme/infra'` —
// and that text becomes an error message, which becomes a line in a window and
// eventually a screenshot in a ticket.
//
// Nothing in this application clones with a URL that has a credential in it,
// so in principle there is nothing to remove. It is removed anyway: git also
// reads `insteadOf` rewrites and credential-helper output from a person's own
// configuration, and either can put a secret into a message that this code
// never assembled. Scrubbing on the way out covers what cannot be checked on
// the way in.
//
// The two passes differ the way the rest of the application treats these URLs.
// An https URL loses its whole userinfo, because a bare username there is
// often the token itself. Any other scheme loses only the password: `git@` in
// an ssh URL is an account name rather than a secret, and removing it would
// change which sentence the reader is looking at.
func Scrub(text string) string {
	text = webUserinfo.ReplaceAllString(text, "$1")
	return otherPassword.ReplaceAllString(text, "$1@")
}
