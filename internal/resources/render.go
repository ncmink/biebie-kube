// Package resources reads Kubernetes objects and turns them into the rows the
// UI renders.
//
// Rendering is driven by the kind catalogue and works on unstructured objects,
// so one code path serves pods, deployments and — later — custom resources
// that have no compiled-in Go type.
package resources

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// renderer turns one object into the kind-specific part of a row.
type renderer func(obj *unstructured.Unstructured) (domain.Health, string, map[string]string)

var renderers = map[domain.Kind]renderer{
	domain.KindPod:                   renderPod,
	domain.KindDeployment:            renderDeployment,
	domain.KindStatefulSet:           renderStatefulSet,
	domain.KindDaemonSet:             renderDaemonSet,
	domain.KindReplicaSet:            renderReplicaSet,
	domain.KindJob:                   renderJob,
	domain.KindCronJob:               renderCronJob,
	domain.KindConfigMap:             renderConfigMap,
	domain.KindSecret:                renderSecret,
	domain.KindPodDisruptionBudget:   renderPDB,
	domain.KindService:               renderService,
	domain.KindIngress:               renderIngress,
	domain.KindPersistentVolumeClaim: renderPVC,
	domain.KindPersistentVolume:      renderPV,
	domain.KindStorageClass:          renderStorageClass,
	domain.KindNamespace:             renderNamespace,
	domain.KindNode:                  renderNode,
	domain.KindEvent:                 renderEvent,
}

// Row renders one object for a table.
func Row(kind domain.Kind, obj *unstructured.Unstructured) domain.ResourceRow {
	row := domain.ResourceRow{
		UID:       string(obj.GetUID()),
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
		CreatedAt: obj.GetCreationTimestamp().Time,
		Health:    domain.HealthUnknown,
	}
	if render, ok := renderers[kind]; ok {
		health, status, fields := render(obj)
		row.Health = health
		row.Status = status
		row.Fields = fields
	}
	// A resource being deleted looks healthy in its own status right up until
	// it disappears, which is misleading while a namespace is stuck
	// terminating.
	if obj.GetDeletionTimestamp() != nil {
		row.Health = domain.HealthWarning
		row.Status = "Terminating"
	}
	return row
}

func renderPod(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	phase := nestedString(obj, "status", "phase")
	statuses := nestedSlice(obj, "status", "containerStatuses")

	ready, total, restarts := 0, len(statuses), int64(0)
	waitingReason := ""
	for _, raw := range statuses {
		container, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if isReady, _, _ := unstructured.NestedBool(container, "ready"); isReady {
			ready++
		}
		if count, found, _ := unstructured.NestedInt64(container, "restartCount"); found {
			restarts += count
		}
		if reason, found, _ := unstructured.NestedString(container, "state", "waiting", "reason"); found && waitingReason == "" {
			waitingReason = reason
		}
		if reason, found, _ := unstructured.NestedString(container, "state", "terminated", "reason"); found &&
			waitingReason == "" && reason != "Completed" {
			waitingReason = reason
		}
	}
	if total == 0 {
		total = len(nestedSlice(obj, "spec", "containers"))
	}

	// A waiting reason is what the engineer needs: "CrashLoopBackOff" says
	// what to do, "Running" on a pod whose container keeps dying does not.
	status := phase
	if waitingReason != "" {
		status = waitingReason
	}

	health := domain.HealthUnknown
	switch {
	case waitingReason != "":
		health = domain.HealthCritical
	case phase == "Running" && ready == total && total > 0:
		health = domain.HealthHealthy
	case phase == "Succeeded":
		health = domain.HealthHealthy
	case phase == "Pending":
		health = domain.HealthProgress
	case phase == "Failed":
		health = domain.HealthCritical
	case phase == "Running":
		health = domain.HealthWarning
	}

	return health, status, map[string]string{
		"ready":    fmt.Sprintf("%d/%d", ready, total),
		"status":   status,
		"restarts": strconv.FormatInt(restarts, 10),
		"node":     nestedString(obj, "spec", "nodeName"),
	}
}

func renderDeployment(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	desired := nestedInt(obj, "spec", "replicas")
	ready := nestedInt(obj, "status", "readyReplicas")
	updated := nestedInt(obj, "status", "updatedReplicas")
	available := nestedInt(obj, "status", "availableReplicas")

	return replicaHealth(ready, desired), fmt.Sprintf("%d/%d", ready, desired), map[string]string{
		"ready":     fmt.Sprintf("%d/%d", ready, desired),
		"upToDate":  strconv.FormatInt(updated, 10),
		"available": strconv.FormatInt(available, 10),
	}
}

func renderStatefulSet(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	desired := nestedInt(obj, "spec", "replicas")
	ready := nestedInt(obj, "status", "readyReplicas")

	image := ""
	if containers := nestedSlice(obj, "spec", "template", "spec", "containers"); len(containers) > 0 {
		if first, ok := containers[0].(map[string]any); ok {
			image, _, _ = unstructured.NestedString(first, "image")
		}
	}

	return replicaHealth(ready, desired), fmt.Sprintf("%d/%d", ready, desired), map[string]string{
		"ready": fmt.Sprintf("%d/%d", ready, desired),
		"image": image,
	}
}

func renderDaemonSet(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	desired := nestedInt(obj, "status", "desiredNumberScheduled")
	ready := nestedInt(obj, "status", "numberReady")
	available := nestedInt(obj, "status", "numberAvailable")

	return replicaHealth(ready, desired), fmt.Sprintf("%d/%d", ready, desired), map[string]string{
		"ready":     fmt.Sprintf("%d/%d", ready, desired),
		"desired":   strconv.FormatInt(desired, 10),
		"available": strconv.FormatInt(available, 10),
	}
}

func renderReplicaSet(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	desired := nestedInt(obj, "spec", "replicas")
	ready := nestedInt(obj, "status", "readyReplicas")

	// A scaled-to-zero replica set is the normal remains of a rollout, not a
	// problem, so it is reported as healthy rather than as zero of zero ready.
	health := replicaHealth(ready, desired)
	if desired == 0 {
		health = domain.HealthHealthy
	}
	return health, fmt.Sprintf("%d/%d", ready, desired), map[string]string{
		"ready":   fmt.Sprintf("%d/%d", ready, desired),
		"desired": strconv.FormatInt(desired, 10),
	}
}

func renderJob(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	succeeded := nestedInt(obj, "status", "succeeded")
	failed := nestedInt(obj, "status", "failed")
	completions := nestedInt(obj, "spec", "completions")
	if completions == 0 {
		completions = 1
	}

	status := "Running"
	health := domain.HealthProgress
	switch {
	case failed > 0:
		status, health = "Failed", domain.HealthCritical
	case succeeded >= completions:
		status, health = "Complete", domain.HealthHealthy
	}

	duration := ""
	if start := nestedString(obj, "status", "startTime"); start != "" {
		if started, err := time.Parse(time.RFC3339, start); err == nil {
			end := time.Now()
			if finish := nestedString(obj, "status", "completionTime"); finish != "" {
				if parsed, err := time.Parse(time.RFC3339, finish); err == nil {
					end = parsed
				}
			}
			duration = ShortDuration(end.Sub(started))
		}
	}

	return health, status, map[string]string{
		"completions": fmt.Sprintf("%d/%d", succeeded, completions),
		"duration":    duration,
	}
}

func renderCronJob(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	suspended, _, _ := unstructured.NestedBool(obj.Object, "spec", "suspend")
	active := len(nestedSlice(obj, "status", "active"))

	status := "Scheduled"
	health := domain.HealthHealthy
	if suspended {
		status, health = "Suspended", domain.HealthWarning
	}

	last := ""
	if raw := nestedString(obj, "status", "lastScheduleTime"); raw != "" {
		if parsed, err := time.Parse(time.RFC3339, raw); err == nil {
			last = ShortDuration(time.Since(parsed)) + " ago"
		}
	}

	return health, status, map[string]string{
		"schedule":     nestedString(obj, "spec", "schedule"),
		"suspend":      boolLabel(suspended),
		"active":       strconv.Itoa(active),
		"lastSchedule": last,
	}
}

func renderConfigMap(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	data, _, _ := unstructured.NestedMap(obj.Object, "data")
	binary, _, _ := unstructured.NestedMap(obj.Object, "binaryData")
	return domain.HealthHealthy, "", map[string]string{
		"keys": strconv.Itoa(len(data) + len(binary)),
	}
}

// renderSecret counts keys and nothing else.
//
// Values are never read here, not even to compute a length: a secret's
// contents must not enter a list response that crosses into the frontend.
func renderSecret(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	data, _, _ := unstructured.NestedMap(obj.Object, "data")
	return domain.HealthHealthy, "", map[string]string{
		"type": nestedString(obj, "type"),
		"keys": strconv.Itoa(len(data)),
	}
}

func renderPDB(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	current := nestedInt(obj, "status", "currentHealthy")
	desired := nestedInt(obj, "status", "desiredHealthy")
	health := domain.HealthHealthy
	if desired > 0 && current < desired {
		health = domain.HealthWarning
	}
	return health, "", map[string]string{
		"minAvailable": displayOrNA(intOrString(obj, "spec", "minAvailable")),
		"healthy":      fmt.Sprintf("%d/%d", current, desired),
	}
}

func renderService(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	serviceType := nestedString(obj, "spec", "type")
	if serviceType == "" {
		serviceType = "ClusterIP"
	}

	ports := make([]string, 0, 4)
	for _, raw := range nestedSlice(obj, "spec", "ports") {
		port, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		number, _, _ := unstructured.NestedInt64(port, "port")
		protocol, _, _ := unstructured.NestedString(port, "protocol")
		if protocol == "" || protocol == "TCP" {
			ports = append(ports, strconv.FormatInt(number, 10))
			continue
		}
		ports = append(ports, fmt.Sprintf("%d/%s", number, protocol))
	}

	health := domain.HealthHealthy
	status := serviceType
	// A LoadBalancer with no address is stuck pending, which looks fine in the
	// list unless it is called out.
	if serviceType == "LoadBalancer" && len(nestedSlice(obj, "status", "loadBalancer", "ingress")) == 0 {
		health, status = domain.HealthProgress, "Pending"
	}

	return health, status, map[string]string{
		"type":      serviceType,
		"clusterIp": nestedString(obj, "spec", "clusterIP"),
		"ports":     strings.Join(ports, ", "),
	}
}

func renderIngress(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	hosts := make([]string, 0, 4)
	for _, raw := range nestedSlice(obj, "spec", "rules") {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if host, found, _ := unstructured.NestedString(rule, "host"); found && host != "" {
			hosts = append(hosts, host)
		}
	}
	return domain.HealthHealthy, "", map[string]string{
		"class": nestedString(obj, "spec", "ingressClassName"),
		"hosts": strings.Join(hosts, ", "),
	}
}

func renderPVC(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	phase := nestedString(obj, "status", "phase")
	health := domain.HealthWarning
	switch phase {
	case "Bound":
		health = domain.HealthHealthy
	case "Pending":
		health = domain.HealthProgress
	case "Lost":
		health = domain.HealthCritical
	}
	return health, phase, map[string]string{
		"status":       phase,
		"capacity":     nestedString(obj, "status", "capacity", "storage"),
		"storageClass": nestedString(obj, "spec", "storageClassName"),
	}
}

func renderPV(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	phase := nestedString(obj, "status", "phase")
	health := domain.HealthWarning
	switch phase {
	case "Bound", "Available":
		health = domain.HealthHealthy
	case "Failed":
		health = domain.HealthCritical
	}

	claim := ""
	if name := nestedString(obj, "spec", "claimRef", "name"); name != "" {
		claim = nestedString(obj, "spec", "claimRef", "namespace") + "/" + name
	}

	return health, phase, map[string]string{
		"status":   phase,
		"capacity": nestedString(obj, "spec", "capacity", "storage"),
		"claim":    claim,
	}
}

func renderStorageClass(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	return domain.HealthHealthy, "", map[string]string{
		"provisioner": nestedString(obj, "provisioner"),
	}
}

func renderNamespace(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	phase := nestedString(obj, "status", "phase")
	health := domain.HealthHealthy
	if phase != "Active" {
		health = domain.HealthWarning
	}
	return health, phase, map[string]string{"status": phase}
}

func renderNode(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	ready, health := "Unknown", domain.HealthUnknown
	for _, raw := range nestedSlice(obj, "status", "conditions") {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		conditionType, _, _ := unstructured.NestedString(condition, "type")
		if conditionType != "Ready" {
			continue
		}
		switch status, _, _ := unstructured.NestedString(condition, "status"); status {
		case "True":
			ready, health = "Ready", domain.HealthHealthy
		case "False":
			ready, health = "NotReady", domain.HealthCritical
		default:
			ready, health = "Unknown", domain.HealthWarning
		}
	}

	// A cordoned node still reports Ready, which hides the reason pods are not
	// being scheduled onto it.
	if unschedulable, _, _ := unstructured.NestedBool(obj.Object, "spec", "unschedulable"); unschedulable {
		ready = ready + ", cordoned"
		if health == domain.HealthHealthy {
			health = domain.HealthWarning
		}
	}

	roles := make([]string, 0, 2)
	for label := range obj.GetLabels() {
		if role, found := strings.CutPrefix(label, "node-role.kubernetes.io/"); found && role != "" {
			roles = append(roles, role)
		}
	}
	sort.Strings(roles)

	return health, ready, map[string]string{
		"status":  ready,
		"roles":   strings.Join(roles, ", "),
		"version": nestedString(obj, "status", "nodeInfo", "kubeletVersion"),
		"cpu":     nestedString(obj, "status", "capacity", "cpu"),
		"memory":  humanQuantity(nestedString(obj, "status", "capacity", "memory")),
	}
}

func renderEvent(obj *unstructured.Unstructured) (domain.Health, string, map[string]string) {
	eventType := nestedString(obj, "type")
	health := domain.HealthHealthy
	if eventType == "Warning" {
		health = domain.HealthWarning
	}

	object := nestedString(obj, "involvedObject", "kind")
	if name := nestedString(obj, "involvedObject", "name"); name != "" {
		object = object + "/" + name
	}

	return health, eventType, map[string]string{
		"type":    eventType,
		"reason":  nestedString(obj, "reason"),
		"object":  object,
		"message": nestedString(obj, "message"),
		"count":   strconv.FormatInt(nestedInt(obj, "count"), 10),
	}
}

func replicaHealth(ready, desired int64) domain.Health {
	switch {
	case desired == 0:
		return domain.HealthWarning
	case ready == 0:
		return domain.HealthCritical
	case ready < desired:
		return domain.HealthProgress
	default:
		return domain.HealthHealthy
	}
}

func nestedString(obj *unstructured.Unstructured, fields ...string) string {
	value, _, _ := unstructured.NestedString(obj.Object, fields...)
	return value
}

func nestedInt(obj *unstructured.Unstructured, fields ...string) int64 {
	value, _, _ := unstructured.NestedInt64(obj.Object, fields...)
	return value
}

func nestedSlice(obj *unstructured.Unstructured, fields ...string) []any {
	value, _, _ := unstructured.NestedSlice(obj.Object, fields...)
	return value
}

func intOrString(obj *unstructured.Unstructured, fields ...string) string {
	raw, found, _ := unstructured.NestedFieldNoCopy(obj.Object, fields...)
	if !found || raw == nil {
		return ""
	}
	switch v := raw.(type) {
	case string:
		return v
	case int64:
		return strconv.FormatInt(v, 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	default:
		return fmt.Sprint(v)
	}
}

func displayOrNA(value string) string {
	if value == "" {
		return "N/A"
	}
	return value
}

func boolLabel(value bool) string {
	if value {
		return "Yes"
	}
	return "No"
}

// ShortDuration renders an age the way kubectl does: one unit, largest first.
// "3d" is what an engineer scanning a list needs; "3d4h12m9s" is not.
func ShortDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// humanQuantity shortens a Kubernetes memory quantity for a table cell.
func humanQuantity(raw string) string {
	if raw == "" {
		return ""
	}
	if value, found := strings.CutSuffix(raw, "Ki"); found {
		if kib, err := strconv.ParseFloat(value, 64); err == nil {
			return fmt.Sprintf("%.0f Gi", kib/1024/1024)
		}
	}
	return raw
}
