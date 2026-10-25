package ai

import (
	"os"
	"strings"
)

// filteredEnvironment returns a minimal env for untrusted CLIs.
//
// Kube credentials and common secret-bearing variables are stripped so a local
// agent cannot accidentally inherit cluster access from BiebieKube's process.
func filteredEnvironment() []string {
	allowPrefixes := []string{
		"PATH=",
		"HOME=",
		"USER=",
		"LOGNAME=",
		"SHELL=",
		"LANG=",
		"LC_",
		"TMPDIR=",
		"TMP=",
		"TEMP=",
		"APPDATA=",
		"LOCALAPPDATA=",
		"USERPROFILE=",
		"SystemRoot=",
		"COMSPEC=",
		"HTTP_PROXY=",
		"HTTPS_PROXY=",
		"NO_PROXY=",
		"ALL_PROXY=",
	}
	var out []string
	for _, entry := range os.Environ() {
		if dropEnv(entry) {
			continue
		}
		if allowedEnv(entry, allowPrefixes) {
			out = append(out, entry)
		}
	}
	out = append(out, "CI=1")
	return out
}

func dropEnv(entry string) bool {
	key, _, ok := strings.Cut(entry, "=")
	if !ok {
		return true
	}
	upper := strings.ToUpper(key)
	if strings.HasPrefix(upper, "KUBE") {
		return true
	}
	if upper == "KUBECONFIG" {
		return true
	}
	if strings.HasSuffix(upper, "TOKEN") || strings.HasSuffix(upper, "SECRET") || strings.HasSuffix(upper, "PASSWORD") {
		return true
	}
	return false
}

func allowedEnv(entry string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasSuffix(p, "_") {
			if strings.HasPrefix(entry, p) {
				return true
			}
			continue
		}
		if strings.HasPrefix(entry, p) {
			return true
		}
	}
	return false
}
