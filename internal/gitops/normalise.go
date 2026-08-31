package gitops

import (
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	kjson "k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/yaml"

	"biebie-kube/internal/argocd"
)

// This file decides what counts as a difference before anything is compared,
// which makes it the part of the feature most able to lie.
//
// Two opposite mistakes are available. Comparing the raw objects reports a
// difference on every field Kubernetes writes for its own bookkeeping, and a
// panel that always says "47 differences" is a panel nobody reads. Stripping
// everything that appears only in the live object hides real drift, because a
// field somebody deleted from Git and never removed from the cluster looks
// exactly like a field the API server defaulted.
//
// The rule taken here is that a field is removed only when it is known to be
// written by Kubernetes rather than by a person. Everything else is reported,
// including defaults — which is why the difference kinds distinguish "changed"
// from "added in live" rather than counting them together.

// canonicalObject re-decodes a live object so both sides of a comparison carry
// the same Go types.
func canonicalObject(object map[string]any) (map[string]any, error) {
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("read the live object: %w", err)
	}
	return canonical(encoded)
}

// canonicalManifest re-decodes a YAML document the same way.
func canonicalManifest(document []byte) (map[string]any, error) {
	encoded, err := yaml.YAMLToJSON(document)
	if err != nil {
		return nil, fmt.Errorf("this manifest is not valid YAML: %w", err)
	}
	return canonical(encoded)
}

// canonical decodes JSON the way the Kubernetes libraries do.
//
// This is not cosmetic and it is not optional. `replicas: 3` read from a
// manifest through encoding/json arrives as a float64, while the same field
// read from the API server arrives as an int64, because the dynamic client
// decodes whole numbers that way. Compared as they arrive, every integer in
// every manifest in every repository would report as a difference, and the
// feature would be worse than useless — it would be confidently wrong.
//
// Putting both sides through the same decoder means a number means one thing.
func canonical(encoded []byte) (map[string]any, error) {
	var out map[string]any
	if err := kjson.Unmarshal(encoded, &out); err != nil {
		return nil, fmt.Errorf("this manifest is not a Kubernetes object: %w", err)
	}
	if out == nil {
		return nil, fmt.Errorf("this manifest is empty")
	}
	return out, nil
}

// normalise removes from a live object the parts that are not source state.
//
// Each removal below is a claim that Kubernetes wrote the field, not a person.
// The list is short on purpose: every entry added to it is a place where real
// drift could hide, so a field earns its way on by being unambiguous
// bookkeeping rather than by being noisy.
func normalise(live map[string]any, source map[string]any) {
	// Status is the cluster's report on the object and is never source state.
	// It is also the largest single source of noise, since a controller
	// rewrites it continuously.
	unstructured.RemoveNestedField(live, "status")

	for _, field := range [][]string{
		// Identity the API server assigns. A manifest cannot carry a uid,
		// because the uid does not exist until the object does.
		{"metadata", "uid"},

		// The optimistic-concurrency counter. It changes on every write to the
		// object by anything at all.
		{"metadata", "resourceVersion"},

		// A counter the API server increments when spec changes. Bookkeeping
		// about source state rather than part of it.
		{"metadata", "generation"},

		// When the object was created, which is a fact about the cluster.
		{"metadata", "creationTimestamp"},

		// The record of which controller owns which field. It is enormous, it
		// is rewritten constantly, and internal/manifest already strips it
		// from the YAML editor for the same reason.
		{"metadata", "managedFields"},

		// A URL the API server used to serve and now mostly does not.
		{"metadata", "selfLink"},
	} {
		unstructured.RemoveNestedField(live, field...)
	}

	// What kubectl stored the last time somebody applied this object. It is a
	// copy of a previous manifest rather than part of this one, and it would
	// otherwise be reported as one enormous added field.
	unstructured.RemoveNestedField(live, "metadata", "annotations",
		"kubectl.kubernetes.io/last-applied-configuration")

	// Argo CD writes its tracking annotation onto the object after applying
	// it, so it is in the cluster and never in the repository. Every
	// annotation-tracked object in the cluster would otherwise report it.
	unstructured.RemoveNestedField(live, "metadata", "annotations", argocd.TrackingAnnotation)

	// A manifest that names no namespace is placed in the namespace the
	// Application sends it to, which is the namespace this object is in. That
	// is the same rule the document matching uses, and without it every
	// namespace-less manifest — which is most of them — would report its own
	// namespace as drift.
	if !sets(source, "metadata", "namespace") {
		unstructured.RemoveNestedField(live, "metadata", "namespace")
	}

	// Kubernetes serialises `creationTimestamp: null` into the metadata of
	// every embedded object, most visibly a Deployment's pod template. It is
	// an artefact of how the type is marshalled rather than a field anybody
	// set.
	dropNullTimestamps(live)

	// An annotations or labels map emptied by the removals above would itself
	// report as a difference against a manifest that has no such map.
	dropEmptyMaps(live, source)

	// Fields Kubernetes fills in when the source leaves them out. This runs
	// after the removals above because a default is decided by what the source
	// says, and the source has to be in its final shape first.
	applyDefaults(nestedString(live, "kind"), live, source)

	// A source that declares no annotations at all still needs them compared
	// one key at a time, or a single annotation written by a controller
	// arrives as the whole map in one unreadable line — which is exactly how
	// `deployment.kubernetes.io/revision` used to look. An empty map on the
	// source side makes the comparison descend into the live one.
	comparePerKey(live, source)

	// The manifest gets the same treatment for the fields a person may
	// genuinely have written into it — a manifest committed by copying
	// `kubectl get -o yaml` carries a uid and a resourceVersion, and comparing
	// those against an object that has different ones is noise rather than
	// news.
	unstructured.RemoveNestedField(source, "status")
	for _, field := range [][]string{
		{"metadata", "uid"},
		{"metadata", "resourceVersion"},
		{"metadata", "generation"},
		{"metadata", "creationTimestamp"},
		{"metadata", "managedFields"},
		{"metadata", "selfLink"},
	} {
		unstructured.RemoveNestedField(source, field...)
	}
	dropNullTimestamps(source)
}

// Two fields are deliberately not removed, because removing them would hide
// something worth seeing:
//
//   - metadata.deletionTimestamp and metadata.deletionGracePeriodSeconds. An
//     object being deleted is not an object that matches its manifest, and it
//     is exactly the kind of thing somebody opening this panel needs to know.
//     They are reported as added in live, which is what they are.
//
//   - labels the cluster carries and the manifest does not, including
//     `app.kubernetes.io/instance` when Argo CD tracks by label. That label is
//     also what Helm writes on everything it installs, so removing it would
//     mean silently discarding a label a chart genuinely declared.

// comparePerKey gives the source an empty map wherever the cluster has one of
// the string maps that controllers write into, so the comparison reports one
// annotation rather than one annotations block.
//
// Only these two, and only at the top level: they are the maps whose keys are
// independent of each other. An arbitrary object missing on one side is one
// thing missing, and splitting it into its fields would be a longer way of
// saying the same thing.
func comparePerKey(live, source map[string]any) {
	for _, field := range [][]string{
		{"metadata", "annotations"},
		{"metadata", "labels"},
		{"spec", "template", "metadata", "annotations"},
		{"spec", "template", "metadata", "labels"},
	} {
		if sets(source, field...) || !sets(live, field...) {
			continue
		}
		if _, ok := nested(live, field...).(map[string]any); !ok {
			continue
		}
		if err := unstructured.SetNestedMap(source, map[string]any{}, field...); err != nil {
			continue
		}
	}
}

// nestedString reads a top-level string, for the kind that chooses which
// defaults apply.
func nestedString(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}

// sets reports whether a document carries a field at all.
func sets(object map[string]any, path ...string) bool {
	_, found, err := unstructured.NestedFieldNoCopy(object, path...)
	return found && err == nil
}

// dropEmptyMaps removes metadata maps that normalisation emptied.
func dropEmptyMaps(live, source map[string]any) {
	for _, field := range [][]string{
		{"metadata", "annotations"},
		{"metadata", "labels"},
	} {
		value, found, err := unstructured.NestedMap(live, field...)
		if found && err == nil && len(value) == 0 && !sets(source, field...) {
			unstructured.RemoveNestedField(live, field...)
		}
	}
}

// dropNullTimestamps removes every null `creationTimestamp` from a document
// and reports whether doing so left this value an empty map.
//
// Only the null ones: a timestamp with a value in it was set by somebody.
//
// The emptiness matters because of where these appear. `spec.template.metadata`
// on a Deployment usually holds nothing but the null timestamp, so removing it
// and leaving `{}` behind would move the noise rather than remove it — the
// empty map would then be reported as a field the cluster has and the manifest
// does not.
//
// Only a map emptied this way is removed. An empty map somebody wrote means
// something — `emptyDir: {}` is a volume, not an absence — and is left exactly
// where it is.
func dropNullTimestamps(value any) bool {
	object, ok := value.(map[string]any)
	if !ok {
		if list, ok := value.([]any); ok {
			for _, nested := range list {
				dropNullTimestamps(nested)
			}
		}
		return false
	}

	stamp, present := object["creationTimestamp"]
	removed := present && stamp == nil
	if removed {
		delete(object, "creationTimestamp")
	}

	for key, nested := range object {
		if dropNullTimestamps(nested) {
			delete(object, key)
		}
	}
	return removed && len(object) == 0
}
