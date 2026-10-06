package resources

import (
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"biebie-kube/internal/domain"
)

// nodeCapacity aggregates per-node allocatable, requests, limits, usage and pod
// counts from the same node and pod lists Overview already reads.
func nodeCapacity(nodes []corev1.Node, pods []corev1.Pod, usage map[string]corev1.ResourceList) ([]domain.NodeCapacity, int) {
	byNode := make(map[string]*domain.NodeCapacity, len(nodes))
	for _, node := range nodes {
		maxPods := int(node.Status.Allocatable.Pods().Value())
		byNode[node.Name] = &domain.NodeCapacity{
			Name:                   node.Name,
			Ready:                  nodeReady(node),
			Cordoned:               node.Spec.Unschedulable,
			CPUAllocatableMilli:    node.Status.Allocatable.Cpu().MilliValue(),
			MemoryAllocatableBytes: node.Status.Allocatable.Memory().Value(),
			MaxPods:                maxPods,
		}
		if used, ok := usage[node.Name]; ok {
			byNode[node.Name].CPUUsedMilli = used.Cpu().MilliValue()
			byNode[node.Name].MemoryUsedBytes = used.Memory().Value()
		}
	}

	unscheduled := 0
	for _, pod := range pods {
		if pod.Status.Phase == corev1.PodPending && pod.Spec.NodeName == "" {
			unscheduled++
			continue
		}
		if !countsTowardNode(pod) {
			continue
		}
		nodeName := pod.Spec.NodeName
		entry, ok := byNode[nodeName]
		if !ok {
			continue
		}
		entry.PodsUsed++
		reqCPU, reqMem, limCPU, limMem := podResources(pod)
		entry.CPURequestMilli += reqCPU.MilliValue()
		entry.MemoryRequestBytes += reqMem.Value()
		entry.CPULimitMilli += limCPU.MilliValue()
		entry.MemoryLimitBytes += limMem.Value()
	}

	out := make([]domain.NodeCapacity, 0, len(byNode))
	for _, entry := range byNode {
		out = append(out, *entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, unscheduled
}

// countsTowardNode reports whether a pod occupies a slot on its node, matching
// the scheduler: assigned, and not finished.
func countsTowardNode(pod corev1.Pod) bool {
	return pod.Spec.NodeName != "" &&
		pod.Status.Phase != corev1.PodSucceeded &&
		pod.Status.Phase != corev1.PodFailed
}

// podResources returns the effective requests and limits for one pod, matching
// the scheduler: max(sum(containers), max(initContainers)) + overhead.
func podResources(pod corev1.Pod) (cpuReq, memReq, cpuLim, memLim resource.Quantity) {
	var containersCPU, containersMem, containersLimCPU, containersLimMem resource.Quantity
	var maxInitCPU, maxInitMem, maxInitLimCPU, maxInitLimMem resource.Quantity

	for _, container := range pod.Spec.Containers {
		containersCPU.Add(*container.Resources.Requests.Cpu())
		containersMem.Add(*container.Resources.Requests.Memory())
		containersLimCPU.Add(*container.Resources.Limits.Cpu())
		containersLimMem.Add(*container.Resources.Limits.Memory())
	}
	for _, container := range pod.Spec.InitContainers {
		if container.Resources.Requests.Cpu().Cmp(maxInitCPU) > 0 {
			maxInitCPU = *container.Resources.Requests.Cpu()
		}
		if container.Resources.Requests.Memory().Cmp(maxInitMem) > 0 {
			maxInitMem = *container.Resources.Requests.Memory()
		}
		if container.Resources.Limits.Cpu().Cmp(maxInitLimCPU) > 0 {
			maxInitLimCPU = *container.Resources.Limits.Cpu()
		}
		if container.Resources.Limits.Memory().Cmp(maxInitLimMem) > 0 {
			maxInitLimMem = *container.Resources.Limits.Memory()
		}
	}

	cpuReq = containersCPU
	if maxInitCPU.Cmp(cpuReq) > 0 {
		cpuReq = maxInitCPU
	}
	memReq = containersMem
	if maxInitMem.Cmp(memReq) > 0 {
		memReq = maxInitMem
	}
	cpuLim = containersLimCPU
	if maxInitLimCPU.Cmp(cpuLim) > 0 {
		cpuLim = maxInitLimCPU
	}
	memLim = containersLimMem
	if maxInitLimMem.Cmp(memLim) > 0 {
		memLim = maxInitLimMem
	}

	cpuReq.Add(*pod.Spec.Overhead.Cpu())
	memReq.Add(*pod.Spec.Overhead.Memory())
	cpuLim.Add(*pod.Spec.Overhead.Cpu())
	memLim.Add(*pod.Spec.Overhead.Memory())
	return cpuReq, memReq, cpuLim, memLim
}

func formatLimits(cpuMilli, memBytes int64, allocCPU int64, allocMem int64) string {
	cpu := formatCPU(cpuMilli)
	mem := formatMemory(memBytes)
	if allocCPU <= 0 && allocMem <= 0 {
		return fmt.Sprintf("%s, %s", cpu, mem)
	}
	cpuPct := percent(cpuMilli, allocCPU)
	memPct := percent(memBytes, allocMem)
	cpuNote := ""
	memNote := ""
	if allocCPU > 0 && cpuMilli > allocCPU {
		cpuNote = " overcommit"
	}
	if allocMem > 0 && memBytes > allocMem {
		memNote = " overcommit"
	}
	return fmt.Sprintf("%s (%s%%%s), %s (%s%%%s)", cpu, cpuPct, cpuNote, mem, memPct, memNote)
}

func formatAllocated(cpuMilli, memBytes int64, allocCPU int64, allocMem int64) string {
	cpu := formatCPU(cpuMilli)
	mem := formatMemory(memBytes)
	if allocCPU <= 0 && allocMem <= 0 {
		return fmt.Sprintf("%s, %s", cpu, mem)
	}
	cpuPct := percent(cpuMilli, allocCPU)
	memPct := percent(memBytes, allocMem)
	return fmt.Sprintf("%s (%s%%), %s (%s%%)", cpu, cpuPct, mem, memPct)
}

func formatPodsUsed(used, max int) string {
	if max <= 0 {
		return fmt.Sprintf("%d", used)
	}
	free := max - used
	if free < 0 {
		free = 0
	}
	return fmt.Sprintf("%d/%d (%d free)", used, max, free)
}

func percent(used, total int64) string {
	if total <= 0 {
		return "—"
	}
	pct := float64(used) / float64(total) * 100
	if pct >= 100 {
		return trimZeros(fmt.Sprintf("%.0f", pct))
	}
	return trimZeros(fmt.Sprintf("%.1f", pct))
}
