package resources

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"

	"biebie-kube/internal/domain"
)

var gpuResourcePrefixes = []string{
	"nvidia.com/",
	"amd.com/",
	"gpu.intel.com/",
	"habana.ai/",
	"google.com/tpu",
}

// isGPUResource reports whether a resource name is a known accelerator extended
// resource on nodes and pods.
func isGPUResource(name corev1.ResourceName) bool {
	s := string(name)
	if strings.HasPrefix(s, "hugepages-") || strings.HasPrefix(s, "attachable-volumes-") {
		return false
	}
	switch name {
	case corev1.ResourceCPU, corev1.ResourceMemory, corev1.ResourcePods, corev1.ResourceEphemeralStorage:
		return false
	}
	for _, prefix := range gpuResourcePrefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

// IsGPUResourceName is the string form for event parsing and other packages.
func IsGPUResourceName(name string) bool {
	return isGPUResource(corev1.ResourceName(name))
}

func gpuFromResourceList(list corev1.ResourceList) map[string]int64 {
	out := make(map[string]int64)
	for name, qty := range list {
		if !isGPUResource(name) {
			continue
		}
		out[string(name)] = qty.Value()
	}
	return out
}

func gpuAllocatable(node corev1.Node) map[string]int64 {
	return gpuFromResourceList(node.Status.Allocatable)
}

// podGPURequests returns effective GPU requests per resource name, using the
// same max(init, sum(containers)) rule as CPU and memory.
func podGPURequests(pod corev1.Pod) map[string]int64 {
	containers := make(map[string]int64)
	for _, container := range pod.Spec.Containers {
		for name, qty := range container.Resources.Requests {
			if !isGPUResource(name) {
				continue
			}
			key := string(name)
			containers[key] += qty.Value()
		}
	}

	initMax := make(map[string]int64)
	for _, container := range pod.Spec.InitContainers {
		for name, qty := range container.Resources.Requests {
			if !isGPUResource(name) {
				continue
			}
			key := string(name)
			if qty.Value() > initMax[key] {
				initMax[key] = qty.Value()
			}
		}
	}

	for key, value := range initMax {
		if value > containers[key] {
			containers[key] = value
		}
	}
	return containers
}

func podRequestsGPU(pod corev1.Pod) bool {
	return len(podGPURequests(pod)) > 0
}

func mergeGPURequested(into map[string]int64, add map[string]int64) {
	for name, qty := range add {
		into[name] += qty
	}
}

func nodeGPUCapacity(alloc map[string]int64, requested map[string]int64) []domain.GPUResource {
	if len(alloc) == 0 && len(requested) == 0 {
		return nil
	}
	names := make(map[string]bool)
	for name := range alloc {
		names[name] = true
	}
	for name := range requested {
		names[name] = true
	}
	keys := make([]string, 0, len(names))
	for name := range names {
		keys = append(keys, name)
	}
	sort.Strings(keys)

	out := make([]domain.GPUResource, 0, len(keys))
	for _, name := range keys {
		out = append(out, domain.GPUResource{
			Name:        name,
			Allocatable: alloc[name],
			Requested:   requested[name],
		})
	}
	return out
}

func formatGPUUsed(used, max int64) string {
	if max <= 0 {
		return formatInt64(used)
	}
	free := max - used
	if free < 0 {
		free = 0
	}
	return formatInt64(used) + "/" + formatInt64(max) + " (" + formatInt64(free) + " free)"
}

func formatGPURequests(requests map[string]int64) string {
	if len(requests) == 0 {
		return ""
	}
	keys := make([]string, 0, len(requests))
	for name := range requests {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	var lines []string
	for _, name := range keys {
		lines = append(lines, name+": "+formatInt64(requests[name]))
	}
	return strings.Join(lines, "\n")
}

func formatInt64(v int64) string {
	return fmt.Sprintf("%d", v)
}
