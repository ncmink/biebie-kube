package git

import "os"

// environment keeps a person's own git configuration and closes the two doors
// that would make this process hang.
//
// The configuration is kept deliberately. A credential helper, an ssh key and
// an `insteadOf` rewrite are how somebody's git reaches their repositories,
// and an application that ignored them would have to ask for a credential of
// its own — which is the thing this package exists to avoid.
//
// What is closed is anything that waits for a person. This git runs behind a
// button in a window: it has no terminal to prompt on, and a graphical
// passphrase dialog raised by a background process is worse than a refusal,
// because the refusal at least says what to fix.
func environment() []string {
	env := append(os.Environ(),
		// No terminal to ask on, so do not stop and try.
		"GIT_TERMINAL_PROMPT=0",

		// And no graphical prompt either, for the same reason.
		"SSH_ASKPASS_REQUIRE=never",

		// classify tells an authentication failure from an unreachable host
		// by reading git's own words. A translated message would match none of
		// them and every failure would read as "something went wrong".
		"LC_ALL=C",
	)

	// Only when the person has not chosen one: theirs may carry the proxy
	// command or the identity file that makes their hosts reachable at all.
	if os.Getenv("GIT_SSH_COMMAND") == "" {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return env
}
