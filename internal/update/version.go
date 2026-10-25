package update

import (
	"strconv"
	"strings"
)

// CompareVersions compares two application version strings.
//
// Leading "v" is ignored. Versions are compared as major.minor.patch
// numeric tuples. Returns -1 if a < b, 0 if equal, 1 if a > b.
// Non-numeric suffixes on the last segment are stripped before parsing.
func CompareVersions(a, b string) int {
	pa := parseVersion(normalizeVersion(a))
	pb := parseVersion(normalizeVersion(b))
	for i := 0; i < 3; i++ {
		if pa[i] < pb[i] {
			return -1
		}
		if pa[i] > pb[i] {
			return 1
		}
	}
	return 0
}

func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.ToLower(v), "v")
	return v
}

func parseVersion(v string) [3]int {
	out := [3]int{0, 0, 0}
	if v == "" {
		return out
	}
	parts := strings.Split(v, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		segment := parts[i]
		if i == len(parts)-1 {
			segment = strings.TrimFunc(segment, func(r rune) bool {
				return r < '0' || r > '9'
			})
		}
		if segment == "" {
			continue
		}
		n, err := strconv.Atoi(segment)
		if err != nil {
			continue
		}
		out[i] = n
	}
	return out
}
