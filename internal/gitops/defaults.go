package gitops

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// This file is normalisation, not classification, and the distinction is the
// whole point of it.
//
// A Deployment whose source omits `replicas` and whose live object says
// `replicas: 1` are not two states, one of which is excusable. They are one
// state written two ways, because Kubernetes defaults the field to 1. Emitting
// that as a difference and then hiding it behind a disclosure would still be
// telling the reader something untrue — it would just be doing it more quietly.
//
// So a default is removed here and never becomes a difference at all. What
// survives into the comparison genuinely exists, and classification's job is
// then to explain it rather than to excuse it.
//
// A default is removed only when both of these hold:
//
//   - the source does not set the field, so there is no disagreement to erase
//   - the live value is exactly the documented default, so nothing a person
//     chose is being concealed
//
// A field set to anything other than its default still shows, which is what
// keeps `replicas: 2` in Git against `replicas: 1` in the cluster visible while
// `replicas` omitted against `replicas: 1` disappears.

// rule is one field Kubernetes fills in when a manifest leaves it out.
type rule struct {
	path  []string
	value any
}

// deploymentDefaults are what the API server writes into a Deployment's spec.
//
// The strategy is listed field by field rather than as one object so that a
// source setting only `strategy.type` still has its percentages defaulted away.
// Removing the three leaves `strategy` empty, and the pruning below takes the
// empty object with it.
var deploymentDefaults = []rule{
	{path: []string{"spec", "replicas"}, value: int64(1)},
	{path: []string{"spec", "progressDeadlineSeconds"}, value: int64(600)},
	{path: []string{"spec", "revisionHistoryLimit"}, value: int64(10)},
	{path: []string{"spec", "strategy", "type"}, value: "RollingUpdate"},
	{path: []string{"spec", "strategy", "rollingUpdate", "maxSurge"}, value: "25%"},
	{path: []string{"spec", "strategy", "rollingUpdate", "maxUnavailable"}, value: "25%"},
}

// podSpecDefaults are what the API server writes into any pod spec.
//
// They are kept separate from the Deployment's own defaults because every
// workload kind embeds a pod template, and a StatefulSet or a Job added later
// should reuse this list rather than restate it.
var podSpecDefaults = []rule{
	{path: []string{"restartPolicy"}, value: "Always"},
	{path: []string{"dnsPolicy"}, value: "ClusterFirst"},
	{path: []string{"schedulerName"}, value: "default-scheduler"},
	{path: []string{"terminationGracePeriodSeconds"}, value: int64(30)},

	// An empty security context is what the API server serialises when nothing
	// asked for one. A context with anything in it is a decision and stays.
	{path: []string{"securityContext"}, value: map[string]any{}},

	// Both spellings of the same field; `serviceAccount` is the deprecated
	// alias the API server still fills in alongside the current one.
	{path: []string{"serviceAccountName"}, value: "default"},
	{path: []string{"serviceAccount"}, value: "default"},
}

// containerDefaults are what the API server writes into every container.
var containerDefaults = []rule{
	{path: []string{"terminationMessagePath"}, value: "/dev/termination-log"},
	{path: []string{"terminationMessagePolicy"}, value: "File"},
	{path: []string{"resources"}, value: map[string]any{}},
}

// applyDefaults removes the fields the source omits and Kubernetes fills in.
//
// Deliberately kind-aware and deliberately short. A generic table of every
// default in the Kubernetes API would be a large thing that is wrong in places
// nobody would notice, and being wrong here means hiding drift. Kinds are added
// when somebody has read the API reference for them.
func applyDefaults(kind string, live, source map[string]any) {
	switch kind {
	case "Deployment":
		defaultFields(live, source, nil, deploymentDefaults)
		defaultPodSpec(live, source, []string{"spec", "template", "spec"})
	}
}

// defaultPodSpec normalises one pod spec wherever it sits.
func defaultPodSpec(live, source map[string]any, prefix []string) {
	defaultFields(live, source, prefix, podSpecDefaults)

	for _, field := range []string{"containers", "initContainers"} {
		defaultList(live, source, prefix, field, "name", func(liveItem, sourceItem map[string]any) {
			defaultFields(liveItem, sourceItem, nil, containerDefaults)
			defaultPullPolicy(liveItem, sourceItem)
			defaultList(liveItem, sourceItem, nil, "ports", "containerPort",
				func(livePort, sourcePort map[string]any) {
					// Every container port the API server returns carries a
					// protocol whether or not the manifest named one.
					defaultFields(livePort, sourcePort, nil, []rule{
						{path: []string{"protocol"}, value: "TCP"},
					})
				})
		})
	}

	defaultList(live, source, prefix, "volumes", "name", func(liveItem, sourceItem map[string]any) {
		// A hostPath with no type means "no checks", which the API server
		// serialises as the empty string rather than by leaving it out. This
		// is why `missing == empty string` is decided per field: it is true
		// here and false for a great many other fields, so it is written down
		// here rather than applied everywhere.
		defaultFields(liveItem, sourceItem, []string{"hostPath"}, []rule{
			{path: []string{"type"}, value: ""},
		})
	})
}

// defaultPullPolicy removes the image pull policy Kubernetes derives.
//
// It is the one default here that depends on another field: a container whose
// image has no tag, or the tag `latest`, is pulled every time, and anything
// else is pulled only when missing. Both are computed rather than fixed, so a
// table entry could not express it.
func defaultPullPolicy(live, source map[string]any) {
	if sets(source, "imagePullPolicy") {
		return
	}
	image, _ := live["image"].(string)
	expected := "IfNotPresent"
	if tag := image[strings.LastIndex(image, "/")+1:]; !strings.Contains(tag, ":") ||
		strings.HasSuffix(tag, ":latest") {
		expected = "Always"
	}
	if live["imagePullPolicy"] == expected {
		delete(live, "imagePullPolicy")
	}
}

// defaultList applies a normaliser to each element of a keyed list, pairing
// elements with their counterpart in the source by the same key the comparison
// uses. An element the source does not declare gets an absent counterpart
// rather than being skipped, so a sidecar the source never mentions still has
// its defaults removed and shows up as one added container instead of one
// container plus six defaulted fields.
func defaultList(live, source map[string]any, prefix []string, field, key string,
	apply func(liveItem, sourceItem map[string]any)) {

	list, ok := nested(live, append(prefix, field)...).([]any)
	if !ok {
		return
	}
	counterparts, _ := nested(source, append(prefix, field)...).([]any)

	for _, element := range list {
		item, ok := element.(map[string]any)
		if !ok {
			continue
		}
		apply(item, counterpart(counterparts, key, item[key]))
	}
}

// counterpart finds the source element with the same key value, or an empty
// map standing for "the source does not declare this one".
func counterpart(list []any, key string, value any) map[string]any {
	for _, element := range list {
		if item, ok := element.(map[string]any); ok && equal(item[key], value) {
			return item
		}
	}
	return map[string]any{}
}

// defaultFields removes the rules that apply, and any object a removal emptied.
func defaultFields(live, source map[string]any, prefix []string, rules []rule) {
	for _, applies := range rules {
		path := append(append([]string{}, prefix...), applies.path...)
		if sets(source, path...) {
			// The source has an opinion about this field. Whether it agrees
			// with the cluster is the comparison's business, not this file's.
			continue
		}
		if !equal(nested(live, path...), applies.value) {
			continue
		}
		unstructured.RemoveNestedField(live, path...)
		prune(live, source, path)
	}
}

// prune removes the objects a removal left empty.
//
// Without it, taking the three defaulted fields out of a Deployment's strategy
// would leave `spec.strategy: {}` behind, and the noise would have moved rather
// than gone. It stops at the first object that still holds something, and at
// anything the source declares.
func prune(live, source map[string]any, path []string) {
	for depth := len(path) - 1; depth > 0; depth-- {
		parent := path[:depth]
		object, ok := nested(live, parent...).(map[string]any)
		if !ok || len(object) > 0 || sets(source, parent...) {
			return
		}
		unstructured.RemoveNestedField(live, parent...)
	}
}

// nested reads a field without copying it, so callers can mutate what they get.
func nested(object map[string]any, path ...string) any {
	value, found, err := unstructured.NestedFieldNoCopy(object, path...)
	if !found || err != nil {
		return nil
	}
	return value
}
