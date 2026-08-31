package gitops

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"biebie-kube/internal/domain"
)

// valueLimit caps how much of one value is carried across the binding.
//
// A difference exists to be read. A field holding an embedded configuration
// file is still one difference, and sending a page of it would tell the reader
// nothing the path did not already say.
const valueLimit = 240

// differenceLimit caps how many differences one comparison reports.
//
// An object compared against a manifest for something else — a name matched by
// coincidence, a repository half-migrated — differs in every field. The
// interesting cases are the small ones, and a list of four hundred rows is a
// list nobody scrolls.
const differenceLimit = 200

// mergeKeys are the list fields whose elements name themselves.
//
// Without this, moving a container from second place to first reports every
// field of both containers as changed, which is the failure that makes a
// comparison feature untrustworthy: the reader learns to ignore it.
//
// This is not a strategic-merge implementation and is not trying to be. It is
// a short table of the lists where the semantics are unarguable, taken from
// the merge keys Kubernetes itself declares for them. A field not on the list
// is compared by position, which is correct for the lists that are genuinely
// ordered — `command`, `args`, `tolerations` — and merely unhelpful for
// anything else.
var mergeKeys = map[string][]string{
	"containers":          {"name"},
	"initContainers":      {"name"},
	"ephemeralContainers": {"name"},
	"env":                 {"name"},
	"volumes":             {"name"},
	"volumeMounts":        {"mountPath"},
	"imagePullSecrets":    {"name"},

	// A port list is keyed differently depending on whether it belongs to a
	// container or to a Service, and both spellings may be absent when the
	// port has no name. The candidates are tried in turn and the first that
	// identifies every element on both sides is used.
	"ports": {"name", "containerPort", "port"},
}

// compare walks two normalised objects and reports where they disagree.
//
// Both are maps of the same shape by this point: same decoder, same
// bookkeeping removed. What is left is a difference somebody could act on, or
// a default the cluster filled in, and the kind on each row says which of
// those two it might be without pretending to know.
func compare(source, live map[string]any, secret bool) []domain.StateDifference {
	out := &differences{secret: secret}
	out.walk("", source, live)
	sort.SliceStable(out.found, func(i, j int) bool { return out.found[i].Path < out.found[j].Path })
	return out.found
}

type differences struct {
	found  []domain.StateDifference
	secret bool
}

func (d *differences) walk(path string, source, live any) {
	if d.full() {
		return
	}

	sourceMap, sourceIsMap := source.(map[string]any)
	liveMap, liveIsMap := live.(map[string]any)
	if sourceIsMap && liveIsMap {
		d.maps(path, sourceMap, liveMap)
		return
	}

	sourceList, sourceIsList := source.([]any)
	liveList, liveIsList := live.([]any)
	if sourceIsList && liveIsList {
		d.lists(path, sourceList, liveList)
		return
	}

	if !equal(source, live) {
		d.add(path, domain.DifferenceChanged, source, live)
	}
}

func (d *differences) maps(path string, source, live map[string]any) {
	for _, key := range sorted(source) {
		want := source[key]
		got, present := live[key]
		if !present {
			d.add(join(path, key), domain.DifferenceMissingInLive, want, nil)
			continue
		}
		d.walk(join(path, key), want, got)
	}
	for _, key := range sorted(live) {
		if _, present := source[key]; !present {
			d.add(join(path, key), domain.DifferenceAddedInLive, nil, live[key])
		}
	}
}

func (d *differences) lists(path string, source, live []any) {
	if pairs, ok := pairUp(field(path), source, live); ok {
		for _, pair := range pairs {
			element := fmt.Sprintf("%s[%s=%s]", path, pair.key, pair.value)
			switch {
			case pair.source == nil:
				d.add(element, domain.DifferenceAddedInLive, nil, pair.live)
			case pair.live == nil:
				d.add(element, domain.DifferenceMissingInLive, pair.source, nil)
			default:
				d.walk(element, pair.source, pair.live)
			}
		}
		return
	}

	// No key that identifies both sides, so position is all there is. That is
	// the right answer for an ordered list and an imprecise one for anything
	// else, and reporting it by index at least says where it looked.
	for index := range max(len(source), len(live)) {
		element := fmt.Sprintf("%s[%d]", path, index)
		switch {
		case index >= len(source):
			d.add(element, domain.DifferenceAddedInLive, nil, live[index])
		case index >= len(live):
			d.add(element, domain.DifferenceMissingInLive, source[index], nil)
		default:
			d.walk(element, source[index], live[index])
		}
	}
}

func (d *differences) add(path string, kind domain.DifferenceKind, source, live any) {
	if d.full() {
		return
	}
	difference := domain.StateDifference{Path: path, Kind: kind}

	if d.secret && secretValue(path) {
		// The field differs and what it differs by does not leave this
		// process. Saying "value differs" is the whole of what a person needs
		// to go and look at the right key.
		difference.Redacted = true
		d.found = append(d.found, difference)
		return
	}

	difference.Source = render(source)
	difference.Live = render(live)
	d.found = append(d.found, difference)
}

func (d *differences) full() bool { return len(d.found) >= differenceLimit }

// secretValue reports a path whose value must not cross the binding.
//
// Only the two maps that hold secret material, and only their contents: the
// key names are what make the difference useful, and `data.DB_PASSWORD` names
// nothing that was not already visible in the resource list.
func secretValue(path string) bool {
	for _, field := range []string{"data", "stringData"} {
		if path == field || strings.HasPrefix(path, field+".") {
			return true
		}
	}
	return false
}

// pair is one element matched between the two lists, either side possibly
// absent.
type pair struct {
	key          string
	value        string
	source, live any
}

// pairUp matches list elements by a key both sides carry.
//
// It refuses unless the key identifies every element uniquely on both sides.
// A half-matched list would be worse than an unmatched one: some rows would be
// compared by name and others by position, and the reader would have no way of
// knowing which.
func pairUp(field string, source, live []any) ([]pair, bool) {
	for _, key := range mergeKeys[field] {
		bySource, ok := index(source, key)
		if !ok {
			continue
		}
		byLive, ok := index(live, key)
		if !ok {
			continue
		}

		var out []pair
		seen := map[string]bool{}
		// Source order first, so the list reads the way the manifest is
		// written rather than the way the cluster happened to store it.
		for _, element := range source {
			value := identifier(element, key)
			seen[value] = true
			out = append(out, pair{key: key, value: value, source: bySource[value], live: byLive[value]})
		}
		for _, element := range live {
			if value := identifier(element, key); !seen[value] {
				out = append(out, pair{key: key, value: value, live: byLive[value]})
			}
		}
		return out, true
	}
	return nil, false
}

// index maps a list to its elements by one key, refusing anything the key does
// not identify.
func index(list []any, key string) (map[string]any, bool) {
	out := make(map[string]any, len(list))
	for _, element := range list {
		object, ok := element.(map[string]any)
		if !ok {
			return nil, false
		}
		value, ok := object[key]
		if !ok {
			return nil, false
		}
		name, ok := scalar(value)
		if !ok || out[name] != nil {
			// A duplicate makes the key meaningless as an identity.
			return nil, false
		}
		out[name] = element
	}
	return out, len(out) > 0
}

func identifier(element any, key string) string {
	object, _ := element.(map[string]any)
	name, _ := scalar(object[key])
	return name
}

// scalar renders a value usable as an identity, refusing maps and lists.
func scalar(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		return typed, typed != ""
	case int64:
		return strconv.FormatInt(typed, 10), true
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64), true
	case bool:
		return strconv.FormatBool(typed), true
	default:
		return "", false
	}
}

// field is the last named segment of a path, which is what the merge-key table
// is keyed by. `spec.template.spec.containers` is a list of containers however
// deep it sits.
func field(path string) string {
	if index := strings.LastIndex(path, "."); index >= 0 {
		path = path[index+1:]
	}
	if index := strings.Index(path, "["); index >= 0 {
		path = path[:index]
	}
	return path
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func sorted(object map[string]any) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// equal compares two leaf values.
//
// Both sides came through the same decoder, so a JSON encoding of each is a
// faithful comparison and needs no reflection over Kubernetes types.
func equal(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return string(left) == string(right)
}

// render turns a value into the short text the panel shows.
func render(value any) string {
	if value == nil {
		return ""
	}
	var out string
	switch typed := value.(type) {
	case string:
		out = typed
	case map[string]any, []any:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		out = string(encoded)
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return ""
		}
		out = string(encoded)
	}
	if len(out) > valueLimit {
		return out[:valueLimit] + "…"
	}
	return out
}
