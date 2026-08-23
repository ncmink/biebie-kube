package cluster

import (
	"strconv"
	"strings"
	"unicode"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// catalogueFor builds the navigation one cluster serves.
//
// This is where the compiled-in catalogue and the cluster's own definitions
// meet, and it is deliberately the only place they do: the resource services
// then work from one list and cannot disagree with the sidebar about whether a
// kind exists.
func catalogueFor(served []kube.APIResource, customs []kube.CustomResource) []domain.KindInfo {
	full := domain.Catalogue()

	// Discovery that returned nothing is not evidence that the cluster serves
	// nothing — an aggregated API server being unhealthy fails the whole call.
	// The built-in kinds stand in that case; custom ones cannot, because there
	// is nothing to have discovered them from.
	out := full
	if len(served) > 0 {
		available := make(map[string]struct{}, len(served))
		for _, resource := range served {
			available[resource.Group+"/"+resource.Resource] = struct{}{}
		}

		out = make([]domain.KindInfo, 0, len(full)+len(customs))
		for _, info := range full {
			if _, ok := available[info.Group+"/"+info.Resource]; ok {
				out = append(out, info)
			}
		}
	}

	for _, custom := range customs {
		out = append(out, customKindInfo(custom))
	}
	return out
}

// customKindInfo turns one definition into a navigable entry.
//
// The title prefers the definition's own kind name over its plural, so the
// sidebar reads "Applications" rather than "applications" — the same word the
// engineer sees in the manifests they wrote.
func customKindInfo(custom kube.CustomResource) domain.KindInfo {
	title := custom.Plural
	if custom.Kind != "" {
		title = plural(custom.Kind)
	}

	info := domain.KindInfo{
		Kind:       domain.CustomKind(custom.Plural, custom.Group),
		Title:      title,
		Category:   domain.CategoryCustom,
		Group:      custom.Group,
		Version:    custom.Version,
		Resource:   custom.Plural,
		Namespaced: custom.Namespaced,
		Custom:     true,
	}

	taken := make(map[string]struct{}, len(custom.Columns))
	for _, column := range custom.Columns {
		info.Columns = append(info.Columns, domain.Column{
			Key:   columnKey(column.Name, taken),
			Title: column.Name,
			Path:  column.JSONPath,
		})
	}
	return info
}

// columnKey derives the field name a rendered value is carried under.
//
// A definition names its columns for people ("Sync Status"), and two of them
// can reduce to the same key, so a repeat is numbered rather than silently
// overwriting the earlier column's value.
func columnKey(name string, taken map[string]struct{}) string {
	var builder strings.Builder
	upperNext := false
	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if builder.Len() == 0 {
				builder.WriteRune(unicode.ToLower(r))
				continue
			}
			if upperNext {
				builder.WriteRune(unicode.ToUpper(r))
				upperNext = false
				continue
			}
			builder.WriteRune(r)
		default:
			upperNext = builder.Len() > 0
		}
	}

	key := builder.String()
	if key == "" {
		key = "value"
	}
	candidate := key
	for i := 2; ; i++ {
		if _, clash := taken[candidate]; !clash {
			break
		}
		candidate = key + strconv.Itoa(i)
	}
	taken[candidate] = struct{}{}
	return candidate
}

// plural turns a Kubernetes kind name into the heading a list page uses.
//
// It handles only what kind names actually contain: they are CamelCase English
// nouns, so the -y and -s endings are the whole of it. Anything cleverer would
// be a guess at a word the cluster owner chose.
func plural(kind string) string {
	switch {
	case strings.HasSuffix(kind, "s"), strings.HasSuffix(kind, "x"),
		strings.HasSuffix(kind, "ch"), strings.HasSuffix(kind, "sh"):
		return kind + "es"
	case strings.HasSuffix(kind, "y") && len(kind) > 1 && !isVowel(kind[len(kind)-2]):
		return kind[:len(kind)-1] + "ies"
	default:
		return kind + "s"
	}
}

func isVowel(c byte) bool {
	switch c {
	case 'a', 'e', 'i', 'o', 'u', 'A', 'E', 'I', 'O', 'U':
		return true
	}
	return false
}
