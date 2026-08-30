package resources

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// Inspect builds the right-hand inspector for one object.
//
// ConfigMap and Secret `data` values are copied as stored. Secret data is
// base64 in the API JSON; this function never decodes it.
func Inspect(kind domain.Kind, obj *unstructured.Unstructured) domain.ResourceInspect {
	out := domain.ResourceInspect{
		Ref: domain.ResourceRef{
			Kind:      kind,
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		},
		CreatedAt:   obj.GetCreationTimestamp().Time,
		Labels:      obj.GetLabels(),
		Annotations: obj.GetAnnotations(),
	}
	if out.Labels == nil {
		out.Labels = map[string]string{}
	}
	if out.Annotations == nil {
		out.Annotations = map[string]string{}
	}

	switch kind {
	case domain.KindSecret:
		out.Type = nestedString(obj, "type")
		out.Data = dataEntries(obj, "data", false)
	case domain.KindConfigMap:
		out.Data = append(dataEntries(obj, "data", false), dataEntries(obj, "binaryData", true)...)
		sort.Slice(out.Data, func(i, j int) bool { return out.Data[i].Key < out.Data[j].Key })
	case domain.KindPodDisruptionBudget:
		out.Properties = inspectPDB(obj)
	case domain.KindDeployment:
		out.Properties = inspectDeployment(obj)
	case domain.KindStatefulSet:
		out.Properties = inspectStatefulSet(obj)
	case domain.KindDaemonSet:
		out.Properties = inspectDaemonSet(obj)
	case domain.KindReplicaSet:
		out.Properties = inspectReplicaSet(obj)
	case domain.KindService:
		out.Properties = inspectService(obj)
	case domain.KindNode:
		out.Properties = inspectNode(obj)
	}

	return out
}

// InspectResource reads one object and returns its inspector payload.
func (s *Service) InspectResource(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.ResourceInspect, error) {
	obj, err := s.Get(ctx, clusterID, ref)
	if err != nil {
		return domain.ResourceInspect{}, err
	}
	return Inspect(ref.Kind, obj), nil
}

func inspectPDB(obj *unstructured.Unstructured) []domain.InspectProperty {
	return []domain.InspectProperty{
		{Label: "Selector", Value: displayOrNA(selectorString(obj)), Mono: true},
		{Label: "Min Available", Value: displayOrNA(intOrString(obj, "spec", "minAvailable"))},
		{Label: "Max Unavailable", Value: displayOrNA(intOrString(obj, "spec", "maxUnavailable"))},
		{Label: "Current Healthy", Value: strconv.FormatInt(nestedInt(obj, "status", "currentHealthy"), 10)},
		{Label: "Desired Healthy", Value: strconv.FormatInt(nestedInt(obj, "status", "desiredHealthy"), 10)},
	}
}

func inspectDeployment(obj *unstructured.Unstructured) []domain.InspectProperty {
	out := props{}
	out.add("Replicas", fmt.Sprintf("%d desired, %d updated, %d total, %d available, %d unavailable",
		nestedInt(obj, "spec", "replicas"),
		nestedInt(obj, "status", "updatedReplicas"),
		nestedInt(obj, "status", "replicas"),
		nestedInt(obj, "status", "availableReplicas"),
		nestedInt(obj, "status", "unavailableReplicas"),
	))
	out.mono("Selector", selectorString(obj))
	out.mono("Node Selector", mapString(obj, "spec", "template", "spec", "nodeSelector"))
	out.add("Strategy Type", nestedString(obj, "spec", "strategy", "type"))
	out.add("Conditions", conditionsString(obj))
	out.addWorkloadScheduling(obj)
	out.mono("Images", imagesString(obj, "spec", "template", "spec"))
	return out
}

func inspectStatefulSet(obj *unstructured.Unstructured) []domain.InspectProperty {
	out := props{}
	out.add("Replicas", fmt.Sprintf("%d desired, %d current, %d ready, %d updated",
		nestedInt(obj, "spec", "replicas"),
		nestedInt(obj, "status", "currentReplicas"),
		nestedInt(obj, "status", "readyReplicas"),
		nestedInt(obj, "status", "updatedReplicas"),
	))
	out.mono("Selector", selectorString(obj))
	out.mono("Node Selector", mapString(obj, "spec", "template", "spec", "nodeSelector"))
	out.add("Service Name", nestedString(obj, "spec", "serviceName"))
	out.add("Update Strategy", nestedString(obj, "spec", "updateStrategy", "type"))
	out.add("Pod Management Policy", nestedString(obj, "spec", "podManagementPolicy"))
	out.add("Conditions", conditionsString(obj))
	out.addWorkloadScheduling(obj)
	out.mono("Images", imagesString(obj, "spec", "template", "spec"))
	return out
}

func inspectDaemonSet(obj *unstructured.Unstructured) []domain.InspectProperty {
	out := props{}
	// A daemon set has no desired replica count of its own: the scheduler
	// decides how many nodes it lands on, so the counts are all status.
	out.add("Pods", fmt.Sprintf("%d desired, %d current, %d ready, %d up-to-date, %d available, %d misscheduled",
		nestedInt(obj, "status", "desiredNumberScheduled"),
		nestedInt(obj, "status", "currentNumberScheduled"),
		nestedInt(obj, "status", "numberReady"),
		nestedInt(obj, "status", "updatedNumberScheduled"),
		nestedInt(obj, "status", "numberAvailable"),
		nestedInt(obj, "status", "numberMisscheduled"),
	))
	out.mono("Selector", selectorString(obj))
	out.mono("Node Selector", mapString(obj, "spec", "template", "spec", "nodeSelector"))
	out.add("Update Strategy", nestedString(obj, "spec", "updateStrategy", "type"))
	out.addWorkloadScheduling(obj)
	out.mono("Images", imagesString(obj, "spec", "template", "spec"))
	return out
}

func inspectReplicaSet(obj *unstructured.Unstructured) []domain.InspectProperty {
	out := props{}
	out.add("Replicas", fmt.Sprintf("%d desired, %d current, %d ready",
		nestedInt(obj, "spec", "replicas"),
		nestedInt(obj, "status", "replicas"),
		nestedInt(obj, "status", "readyReplicas"),
	))
	out.add("Revision", obj.GetAnnotations()[revisionAnnotation])
	out.mono("Selector", selectorString(obj))
	out.mono("Node Selector", mapString(obj, "spec", "template", "spec", "nodeSelector"))
	out.addWorkloadScheduling(obj)
	out.mono("Images", imagesString(obj, "spec", "template", "spec"))
	return out
}

func inspectService(obj *unstructured.Unstructured) []domain.InspectProperty {
	out := props{}
	out.add("Type", nestedString(obj, "spec", "type"))
	out.mono("Cluster IP", nestedString(obj, "spec", "clusterIP"))
	out.mono("External IPs", strings.Join(stringSlice(obj, "spec", "externalIPs"), "\n"))
	out.mono("Ports", servicePortsString(obj))
	// A service selects pods with a flat map, not the LabelSelector every
	// workload uses, so it cannot share selectorString.
	out.mono("Selector", mapString(obj, "spec", "selector"))
	out.add("Session Affinity", nestedString(obj, "spec", "sessionAffinity"))
	return out
}

func inspectNode(obj *unstructured.Unstructured) []domain.InspectProperty {
	out := props{}
	if unschedulable, _, _ := unstructured.NestedBool(obj.Object, "spec", "unschedulable"); unschedulable {
		out.add("Scheduling", "Cordoned")
	}
	out.add("Conditions", conditionsString(obj))
	out.add("Taints", countLabel(len(nestedSlice(obj, "spec", "taints")), "Taint"))
	out.add("Capacity", quantitiesString(obj, "status", "capacity"))
	out.add("Allocatable", quantitiesString(obj, "status", "allocatable"))
	out.mono("Internal IP", nodeAddress(obj, "InternalIP"))
	out.add("Kubelet Version", nestedString(obj, "status", "nodeInfo", "kubeletVersion"))
	out.add("OS Image", nestedString(obj, "status", "nodeInfo", "osImage"))
	out.add("Container Runtime", nestedString(obj, "status", "nodeInfo", "containerRuntimeVersion"))
	out.add("Architecture", nestedString(obj, "status", "nodeInfo", "architecture"))
	return out
}

// props collects inspector rows, dropping the ones with nothing to say.
//
// An absent field is left out rather than shown as "N/A": a drawer of empty
// rows costs the reader the same attention as a full one and tells them less.
type props []domain.InspectProperty

func (p *props) add(label, value string) {
	if value == "" {
		return
	}
	*p = append(*p, domain.InspectProperty{Label: label, Value: value})
}

func (p *props) mono(label, value string) {
	if value == "" {
		return
	}
	*p = append(*p, domain.InspectProperty{Label: label, Value: value, Mono: true})
}

// addWorkloadScheduling adds the pod-template rows that read the same for
// every workload: what keeps its pods off a node, and what keeps them apart.
func (p *props) addWorkloadScheduling(obj *unstructured.Unstructured) {
	p.add("Tolerations", countLabel(len(nestedSlice(obj, "spec", "template", "spec", "tolerations")), "Toleration"))
	p.add("Pod Affinities", affinityRules(obj, "podAffinity"))
	p.add("Pod Anti Affinities", affinityRules(obj, "podAntiAffinity"))
	p.add("Node Affinities", affinityRules(obj, "nodeAffinity"))
}

// selectorString renders a LabelSelector the way kubectl does, one term per
// line so a narrow drawer can hold it.
func selectorString(obj *unstructured.Unstructured) string {
	parts := keyValueLines(obj, "spec", "selector", "matchLabels")
	for _, raw := range nestedSlice(obj, "spec", "selector", "matchExpressions") {
		term, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key, _ := term["key"].(string)
		operator, _ := term["operator"].(string)
		values, _, _ := unstructured.NestedStringSlice(term, "values")
		if len(values) == 0 {
			parts = append(parts, fmt.Sprintf("%s %s", key, operator))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s %s (%s)", key, operator, strings.Join(values, ", ")))
	}
	return strings.Join(parts, "\n")
}

// mapString renders a flat string map — a node selector, a service's selector
// — one entry per line, sorted so it reads the same on every repaint.
func mapString(obj *unstructured.Unstructured, fields ...string) string {
	return strings.Join(keyValueLines(obj, fields...), "\n")
}

func keyValueLines(obj *unstructured.Unstructured, fields ...string) []string {
	entries, _, _ := unstructured.NestedStringMap(obj.Object, fields...)
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+entries[key])
	}
	return lines
}

// conditionsString names the conditions that currently hold.
//
// Only the true ones: a node reporting MemoryPressure=False is reporting that
// nothing is wrong, and listing it would read as though something is.
func conditionsString(obj *unstructured.Unstructured) string {
	var held []string
	for _, raw := range nestedSlice(obj, "status", "conditions") {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if status, _ := condition["status"].(string); status != "True" {
			continue
		}
		if name, _ := condition["type"].(string); name != "" {
			held = append(held, name)
		}
	}
	return strings.Join(held, ", ")
}

// imagesString lists the images a pod template runs, init containers first,
// with duplicates collapsed — a sidecar shared by three containers is one
// image to pull and one image to patch.
func imagesString(obj *unstructured.Unstructured, podSpec ...string) string {
	var images []string
	seen := map[string]bool{}
	for _, field := range []string{"initContainers", "containers"} {
		for _, raw := range nestedSlice(obj, append(append([]string{}, podSpec...), field)...) {
			container, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			image, _ := container["image"].(string)
			if image == "" || seen[image] {
				continue
			}
			seen[image] = true
			images = append(images, image)
		}
	}
	return strings.Join(images, "\n")
}

// affinityRules counts a pod template's affinity terms of one flavour, which
// is what an engineer wants at a glance; the terms themselves are in the YAML.
func affinityRules(obj *unstructured.Unstructured, flavour string) string {
	base := []string{"spec", "template", "spec", "affinity", flavour}
	rules := len(nestedSlice(obj, append(append([]string{}, base...), "preferredDuringSchedulingIgnoredDuringExecution")...))
	if flavour == "nodeAffinity" {
		terms := append(append([]string{}, base...), "requiredDuringSchedulingIgnoredDuringExecution", "nodeSelectorTerms")
		rules += len(nestedSlice(obj, terms...))
	} else {
		rules += len(nestedSlice(obj, append(append([]string{}, base...), "requiredDuringSchedulingIgnoredDuringExecution")...))
	}
	if rules == 0 {
		return ""
	}
	return countLabel(rules, "Rule")
}

func servicePortsString(obj *unstructured.Unstructured) string {
	var lines []string
	for _, raw := range nestedSlice(obj, "spec", "ports") {
		port, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		number, _, _ := unstructured.NestedInt64(port, "port")
		protocol, _ := port["protocol"].(string)
		line := fmt.Sprintf("%d/%s", number, protocol)
		if node, found, _ := unstructured.NestedInt64(port, "nodePort"); found && node != 0 {
			line += fmt.Sprintf(":%d", node)
		}
		if name, _ := port["name"].(string); name != "" {
			line = name + " " + line
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// quantitiesString renders a node's capacity or allocatable, keeping to the
// three an engineer sizes against.
func quantitiesString(obj *unstructured.Unstructured, fields ...string) string {
	entries, _, _ := unstructured.NestedStringMap(obj.Object, fields...)
	var parts []string
	for _, key := range []string{"cpu", "memory", "pods"} {
		if value := entries[key]; value != "" {
			parts = append(parts, key+" "+value)
		}
	}
	return strings.Join(parts, ", ")
}

func nodeAddress(obj *unstructured.Unstructured, want string) string {
	for _, raw := range nestedSlice(obj, "status", "addresses") {
		address, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if kind, _ := address["type"].(string); kind == want {
			value, _ := address["address"].(string)
			return value
		}
	}
	return ""
}

func stringSlice(obj *unstructured.Unstructured, fields ...string) []string {
	values, _, _ := unstructured.NestedStringSlice(obj.Object, fields...)
	return values
}

func countLabel(count int, singular string) string {
	if count == 0 {
		return ""
	}
	if count == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(count) + " " + singular + "s"
}

func dataEntries(obj *unstructured.Unstructured, field string, binary bool) []domain.DataEntry {
	raw, ok, _ := unstructured.NestedFieldNoCopy(obj.Object, field)
	data, okMap := raw.(map[string]any)
	if !ok || !okMap || len(data) == 0 {
		return nil
	}
	entries := make([]domain.DataEntry, 0, len(data))
	for key, value := range data {
		entries = append(entries, domain.DataEntry{
			Key:    key,
			Value:  storedString(value),
			Binary: binary,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries
}

// storedString keeps Kubernetes' on-the-wire form.
//
// A string is already what the API JSON carries (base64 for Secret data).
// []byte would be the decoded secret from a typed object — re-encode it so
// the UI never receives plaintext by accident.
func storedString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return base64.StdEncoding.EncodeToString(v)
	default:
		return fmt.Sprint(v)
	}
}
