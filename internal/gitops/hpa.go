package gitops

import (
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// matchingHPAs returns the HorizontalPodAutoscalers in the same namespace
// whose scale target is this object.
//
// Kind and name must both match. An apiVersion, when the HPA names one, must
// at least share a group with the object — matching only the name would let
// an HPA for a identically-named ReplicaSet or a CRD explain a Deployment.
func matchingHPAs(hpas []*unstructured.Unstructured, live *unstructured.Unstructured) []*unstructured.Unstructured {
	if live == nil {
		return nil
	}
	var out []*unstructured.Unstructured
	for _, hpa := range hpas {
		if hpa == nil {
			continue
		}
		if targets(hpa, live) {
			out = append(out, hpa)
		}
	}
	return out
}

func targets(hpa, live *unstructured.Unstructured) bool {
	if hpa.GetNamespace() != live.GetNamespace() {
		return false
	}
	kind, _, _ := unstructured.NestedString(hpa.Object, "spec", "scaleTargetRef", "kind")
	name, _, _ := unstructured.NestedString(hpa.Object, "spec", "scaleTargetRef", "name")
	if kind != live.GetKind() || name != live.GetName() || kind == "" || name == "" {
		return false
	}
	apiVersion, _, _ := unstructured.NestedString(hpa.Object, "spec", "scaleTargetRef", "apiVersion")
	return compatibleAPIVersion(apiVersion, live.GetAPIVersion())
}

// compatibleAPIVersion reports whether a scaleTargetRef apiVersion may refer
// to this object.
//
// An empty ref is accepted: older HPAs omit it and Kubernetes still resolves
// them by kind and name inside the namespace. A ref that names a group must
// name this object's group, so an HPA for a core v1 ReplicationController
// cannot explain an apps Deployment of the same name.
func compatibleAPIVersion(ref, live string) bool {
	if ref == "" {
		return true
	}
	if ref == live {
		return true
	}
	return apiGroup(ref) != "" && apiGroup(ref) == apiGroup(live)
}

func apiGroup(apiVersion string) string {
	if group, _, found := strings.Cut(apiVersion, "/"); found {
		return group
	}
	// A core object is "v1" with no group. That empty group is a real value
	// and must not match "apps".
	return ""
}

func hpaEvidence(hpa *unstructured.Unstructured, live *unstructured.Unstructured) domain.DifferenceEvidence {
	name := hpa.GetName()
	out := domain.DifferenceEvidence{
		Kind:       domain.EvidenceHPATarget,
		Confidence: domain.ConfidenceConfirmed,
		Subject:    name,
		Summary:    "HPA " + name + " targets " + live.GetKind() + "/" + live.GetName() + ".",
		Facts: []domain.EvidenceFact{
			{Name: "Target", Value: live.GetKind() + "/" + live.GetName()},
		},
	}

	if min, ok := nestedNumber(hpa.Object, "spec", "minReplicas"); ok {
		out.Facts = append(out.Facts, domain.EvidenceFact{Name: "Min", Value: min})
	} else {
		// Kubernetes defaults minReplicas to 1 when the field is omitted.
		out.Facts = append(out.Facts, domain.EvidenceFact{Name: "Min", Value: "1"})
	}
	if max, ok := nestedNumber(hpa.Object, "spec", "maxReplicas"); ok {
		out.Facts = append(out.Facts, domain.EvidenceFact{Name: "Max", Value: max})
	}
	if current, ok := nestedNumber(hpa.Object, "status", "currentReplicas"); ok {
		out.Facts = append(out.Facts, domain.EvidenceFact{Name: "Current", Value: current})
	}
	if desired, ok := nestedNumber(hpa.Object, "status", "desiredReplicas"); ok {
		out.Facts = append(out.Facts, domain.EvidenceFact{Name: "Desired", Value: desired})
	}
	if metric := firstMetric(hpa.Object); metric != "" {
		out.Facts = append(out.Facts, domain.EvidenceFact{Name: "Metric", Value: metric})
	}
	return out
}

func firstMetric(object map[string]any) string {
	list, _, _ := unstructured.NestedSlice(object, "status", "currentMetrics")
	if len(list) == 0 {
		return ""
	}
	metric, ok := list[0].(map[string]any)
	if !ok {
		return ""
	}
	if name, _, _ := unstructured.NestedString(metric, "resource", "name"); name != "" {
		return name
	}
	kind, _, _ := unstructured.NestedString(metric, "type")
	return kind
}
