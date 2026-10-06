package incident

import (
	"fmt"
	"strings"

	"biebie-kube/internal/domain"
)

// Analyze runs deterministic rules over collected evidence.
func Analyze(bundle EvidenceBundle) []domain.IncidentFinding {
	var findings []domain.IncidentFinding

	if bundle.Pod != nil {
		findings = append(findings, podRules(bundle)...)
		if len(findings) == 0 && bundle.Pod.Status == "Succeeded" {
			return findings
		}
	}
	if bundle.Node != nil {
		findings = append(findings, nodeRules(bundle)...)
	}

	findings = append(findings, eventRules(bundle)...)

	if len(findings) == 0 && len(bundle.Missing) > 0 {
		findings = append(findings, insufficientEvidenceFinding(bundle))
	}

	return sortFindings(findings)
}

func podRules(bundle EvidenceBundle) []domain.IncidentFinding {
	pod := bundle.Pod
	var out []domain.IncidentFinding

	for _, container := range append(pod.Containers, pod.InitContainers...) {
		switch container.State {
		case "CrashLoopBackOff":
			id := fmt.Sprintf("container.%s.state", container.Name)
			out = append(out, domain.IncidentFinding{
				RuleID:          "pod.crash_loop_backoff",
				Severity:        domain.SeverityCritical,
				Summary:         fmt.Sprintf("%s is in CrashLoopBackOff", container.Name),
				Explanation:     "The container keeps exiting and Kubernetes is backing off before restarting it again.",
				ConfidenceClass: domain.IncidentConfidenceConfirmed,
				ObservedFacts: []string{
					fmt.Sprintf("Container %s state: CrashLoopBackOff", container.Name),
					fmt.Sprintf("Restart count: %d", container.RestartCount),
				},
				PossibleCauses: []string{
					"The application exits on startup or crashes shortly after starting.",
					"A misconfigured command, missing dependency, or failed health check may be causing repeated exits.",
				},
				EvidenceIDs: []string{id},
				NextSteps: []string{
					"Open previous container logs",
					"Inspect container command and environment",
					"Check recent warning events",
				},
			})
		case "ImagePullBackOff", "ErrImagePull":
			id := fmt.Sprintf("container.%s.state", container.Name)
			out = append(out, domain.IncidentFinding{
				RuleID:          "pod.image_pull_backoff",
				Severity:        domain.SeverityCritical,
				Summary:         fmt.Sprintf("%s cannot pull %q", container.Name, container.Image),
				Explanation:     "The node cannot fetch the container image, so the pod cannot start.",
				ConfidenceClass: domain.IncidentConfidenceConfirmed,
				ObservedFacts: []string{
					fmt.Sprintf("Container %s state: %s", container.Name, container.State),
					fmt.Sprintf("Image: %s", container.Image),
				},
				PossibleCauses: []string{
					"The image name or tag is wrong.",
					"Registry credentials or pull secrets may be missing.",
					"The registry may be unreachable from this cluster.",
				},
				EvidenceIDs: []string{id},
				NextSteps: []string{
					"Verify the image reference in the workload manifest",
					"Check image pull secrets and registry access",
					"Inspect warning events for the exact pull error",
				},
			})
		}

		reason := container.LastTerminationReason
		if reason == "OOMKilled" || (reason == "" && container.LastExitCode == 137) {
			id := fmt.Sprintf("container.%s.state", container.Name)
			out = append(out, domain.IncidentFinding{
				RuleID:          "pod.oom_killed",
				Severity:        domain.SeverityCritical,
				Summary:         fmt.Sprintf("%s was OOMKilled", container.Name),
				Explanation:     "The container was terminated after an out-of-memory condition was reported.",
				ConfidenceClass: domain.IncidentConfidenceConfirmed,
				ObservedFacts:   factsForTermination(container),
				PossibleCauses: []string{
					"Memory usage exceeded the configured limit.",
					"A memory leak or traffic spike may have driven usage above the limit.",
				},
				EvidenceIDs: []string{id},
				NextSteps: []string{
					"Open previous container logs",
					"Review memory requests and limits",
					"Inspect memory usage over time",
				},
			})
		}
	}

	for _, condition := range pod.Conditions {
		if condition.Type == "PodScheduled" && condition.Status == "False" {
			id := fmt.Sprintf("condition.%s", condition.Type)
			out = append(out, domain.IncidentFinding{
				RuleID:          "pod.scheduling_failed",
				Severity:        domain.SeverityWarning,
				Summary:         "Pod is not scheduled",
				Explanation:     "The scheduler has not placed this pod on a node.",
				ConfidenceClass: domain.IncidentConfidenceSupported,
				ObservedFacts: []string{
					fmt.Sprintf("Condition PodScheduled=False (%s)", condition.Reason),
					condition.Message,
				},
				PossibleCauses: []string{
					"No node matches selector, taints, or resource requests.",
					"Persistent volume or topology constraints may block placement.",
				},
				EvidenceIDs: []string{id},
				NextSteps: []string{
					"Inspect node capacity and taints",
					"Review pod affinity, selectors, and resource requests",
				},
			})
		}
	}

	return out
}

func nodeRules(bundle EvidenceBundle) []domain.IncidentFinding {
	if bundle.Node == nil {
		return nil
	}
	var out []domain.IncidentFinding
	ready := conditionByType(bundle.Node.Conditions, "Ready")
	if ready != nil && (ready.Status == "False" || ready.Status == "Unknown") {
		out = append(out, nodeNotReady(*ready))
	}
	if networkPluginFailing(bundle) {
		out = append(out, networkPluginNotReady(bundle))
	}
	if finding, ok := networkResourceExhaustion(bundle); ok {
		out = append(out, finding)
	}
	return out
}

func nodeNotReady(ready domain.Condition) domain.IncidentFinding {
	return domain.IncidentFinding{
		RuleID:          "node.not_ready",
		Severity:        domain.SeverityCritical,
		Summary:         "Node is not ready",
		Explanation:     "The kubelet is not reporting this node as Ready, so the scheduler should stop placing new pods here.",
		ConfidenceClass: domain.IncidentConfidenceConfirmed,
		ObservedFacts: []string{
			fmt.Sprintf("Ready=%s", ready.Status),
			fmt.Sprintf("Reason: %s", ready.Reason),
			truncate(ready.Message, 240),
		},
		EvidenceIDs: []string{"condition.Ready"},
		NextSteps: []string{
			"Read the Ready condition message",
			"Inspect kubelet and container runtime logs on the node",
		},
	}
}

func networkPluginNotReady(bundle EvidenceBundle) domain.IncidentFinding {
	facts := make([]string, 0, 4)
	if ready := conditionByType(bundle.Node.Conditions, "Ready"); ready != nil {
		facts = append(facts, fmt.Sprintf("Ready=%s reason=%s", ready.Status, ready.Reason))
		if ready.Message != "" {
			facts = append(facts, truncate(ready.Message, 240))
		}
	}
	if unavailable := conditionByType(bundle.Node.Conditions, "NetworkUnavailable"); unavailable != nil && unavailable.Status == "True" {
		facts = append(facts, fmt.Sprintf("NetworkUnavailable=True (%s)", unavailable.Reason))
	}
	if bundle.NodePods != nil {
		for _, agent := range bundle.NodePods.Agents {
			facts = append(facts, fmt.Sprintf("kube-system DaemonSet pod %s is not ready (%s)", agent.Name, agent.Phase))
		}
	}
	return domain.IncidentFinding{
		RuleID:          "node.network_plugin_not_ready",
		Severity:        domain.SeverityCritical,
		Summary:         "Node network plugin is not ready",
		Explanation:     "The node's network plugin is not running, so pods on this node cannot get networking.",
		ConfidenceClass: domain.IncidentConfidenceConfirmed,
		ObservedFacts:   facts,
		EvidenceIDs:     []string{"condition.Ready"},
		NextSteps: []string{
			"Inspect the network agent pod on this node and its init container logs",
			"Check whether the same agent is healthy on other nodes",
			"Cordon the node so new pods stop landing there",
		},
	}
}

// networkResourceExhaustion is a heuristic. Pod density is recorded when the
// rule fires; it is never enough to fire the rule by itself.
func networkResourceExhaustion(bundle EvidenceBundle) (domain.IncidentFinding, bool) {
	if !networkPluginFailing(bundle) && !hasSandboxFailure(bundle) {
		return domain.IncidentFinding{}, false
	}
	// Absent MemoryPressure is not pressure. Only True explains the
	// allocation failure as ordinary node memory pressure.
	if bundle.Node != nil && memoryPressureTrue(bundle.Node.Conditions) {
		return domain.IncidentFinding{}, false
	}
	quotes := allocationQuotes(bundle)
	if len(quotes) == 0 {
		return domain.IncidentFinding{}, false
	}

	facts := append([]string{}, quotes...)
	if bundle.NodePods != nil && bundle.NodePods.MaxPods > 0 {
		used := bundle.NodePods.PodsUsed
		max := bundle.NodePods.MaxPods
		if float64(used)/float64(max) > 0.8 {
			facts = append(facts, fmt.Sprintf("Pods on this node: %d/%d", used, max))
		}
	}
	return domain.IncidentFinding{
		RuleID:   "node.network_resource_exhaustion",
		Severity: domain.SeverityWarning,
		Summary:  "Network setup fails with allocation errors while the node has free memory",
		Explanation: "The kernel refused to allocate network resources even though " +
			"the node reports no memory pressure. This usually means a kernel or platform " +
			"limit was reached, for example the conntrack table, or a per-container " +
			"iptables quota on container-based hosts such as OpenVZ/Virtuozzo.",
		ConfidenceClass: domain.IncidentConfidenceSupported,
		ObservedFacts:   facts,
		NextSteps: []string{
			"Check kernel network limits on the node (conntrack usage, iptables rule count)",
			"On container-based hosts, check the platform quota (e.g. /proc/user_beancounters, numiptent and failcnt)",
			"Avoid restarting the network agent repeatedly; it fails the same way until the limit is raised or load is reduced",
			"Reduce pods or Services on this node, or ask the platform to raise the limit",
		},
	}, true
}

func networkPluginFailing(bundle EvidenceBundle) bool {
	if bundle.Node == nil {
		return false
	}
	if ready := conditionByType(bundle.Node.Conditions, "Ready"); ready != nil {
		text := ready.Reason + " " + ready.Message
		if containsAny(text, "networkpluginnotready", "network plugin", "cni") {
			return true
		}
	}
	if unavailable := conditionByType(bundle.Node.Conditions, "NetworkUnavailable"); unavailable != nil && unavailable.Status == "True" {
		return true
	}
	return false
}

func memoryPressureTrue(conditions []domain.Condition) bool {
	pressure := conditionByType(conditions, "MemoryPressure")
	return pressure != nil && pressure.Status == "True"
}

func hasSandboxFailure(bundle EvidenceBundle) bool {
	for _, event := range relevantEvents(bundle) {
		if event.Type == "Warning" && event.Reason == "FailedCreatePodSandBox" {
			return true
		}
	}
	return false
}

func allocationQuotes(bundle EvidenceBundle) []string {
	var out []string
	for _, event := range relevantEvents(bundle) {
		if event.Reason != "FailedCreatePodSandBox" || !allocationFailure(event.Message) {
			continue
		}
		out = append(out, truncate(event.Message, 240))
	}
	if bundle.NodePods == nil {
		return out
	}
	for _, agent := range bundle.NodePods.Agents {
		for _, container := range append(agent.InitContainers, agent.Containers...) {
			if !allocationFailure(container.LastTerminationMessage) {
				continue
			}
			out = append(out, fmt.Sprintf("%s: %s", agent.Name, truncate(container.LastTerminationMessage, 240)))
		}
	}
	return out
}

func allocationFailure(text string) bool {
	return containsAny(text,
		"cannot allocate memory",
		"memory allocation problem",
		"no buffer space available",
		"out of memory",
	)
}

func containsAny(text string, phrases ...string) bool {
	folded := strings.ToLower(text)
	for _, phrase := range phrases {
		if strings.Contains(folded, phrase) {
			return true
		}
	}
	return false
}

func conditionByType(conditions []domain.Condition, kind string) *domain.Condition {
	for i := range conditions {
		if conditions[i].Type == kind {
			return &conditions[i]
		}
	}
	return nil
}

func relevantEvents(bundle EvidenceBundle) []domain.EventRow {
	var events []domain.EventRow
	if bundle.EventsOK {
		events = append(events, bundle.Events...)
	}
	return append(events, bundle.PodEvents...)
}

func eventRules(bundle EvidenceBundle) []domain.IncidentFinding {
	events := relevantEvents(bundle)
	if len(events) == 0 {
		return nil
	}
	var out []domain.IncidentFinding
	for _, event := range events {
		if event.Type != "Warning" {
			continue
		}
		switch event.Reason {
		case "FailedScheduling":
			id := "event." + event.UID
			out = append(out, domain.IncidentFinding{
				RuleID:          "event.failed_scheduling",
				Severity:        domain.SeverityWarning,
				Summary:         "Scheduling failed",
				Explanation:     event.Message,
				ConfidenceClass: domain.IncidentConfidenceSupported,
				ObservedFacts:   []string{fmt.Sprintf("Warning event: %s", event.Reason)},
				EvidenceIDs:     []string{id},
				NextSteps:       []string{"Review node capacity, taints, and pod constraints"},
			})
		case "FailedCreatePodSandBox":
			id := "event." + event.UID
			explanation := event.Message
			if containsAny(event.Message, "network", "cni", "plugin") {
				explanation = "The pod sandbox could not be created because network setup failed. " + event.Message
			}
			out = append(out, domain.IncidentFinding{
				RuleID:          "event.pod_sandbox_failed",
				Severity:        domain.SeverityCritical,
				Summary:         "Pod sandbox creation failed",
				Explanation:     explanation,
				ConfidenceClass: domain.IncidentConfidenceSupported,
				ObservedFacts:   []string{fmt.Sprintf("Warning event: %s", event.Reason)},
				EvidenceIDs:     []string{id},
				NextSteps:       []string{"Inspect the network agent pod on this node and its init container logs"},
			})
		case "FailedMount", "FailedAttachVolume":
			id := "event." + event.UID
			out = append(out, domain.IncidentFinding{
				RuleID:          "event.volume_mount_failed",
				Severity:        domain.SeverityCritical,
				Summary:         "Volume mount failed",
				Explanation:     event.Message,
				ConfidenceClass: domain.IncidentConfidenceSupported,
				ObservedFacts:   []string{fmt.Sprintf("Warning event: %s", event.Reason)},
				EvidenceIDs:     []string{id},
				NextSteps:       []string{"Inspect PVC binding and storage class", "Check volume attachment on the node"},
			})
		}
	}
	return out
}

func insufficientEvidenceFinding(bundle EvidenceBundle) domain.IncidentFinding {
	return domain.IncidentFinding{
		RuleID:          "report.insufficient_evidence",
		Severity:        domain.SeverityInfo,
		Summary:         "Unable to determine a supported cause",
		Explanation:     "The available evidence is insufficient to explain the current state without guessing.",
		ConfidenceClass: domain.IncidentConfidenceUnknown,
		ObservedFacts:   scopesAsFacts(bundle.Scopes),
		NextSteps: []string{
			"Refresh this report after permissions or related objects change",
			"Inspect previous container logs",
			"Review warning events",
		},
	}
}

func factsForTermination(container domain.ContainerInfo) []string {
	facts := []string{
		fmt.Sprintf("Last termination reason: %s", container.LastTerminationReason),
	}
	if container.LastExitCode != 0 {
		facts = append(facts, fmt.Sprintf("Exit code: %d", container.LastExitCode))
	}
	facts = append(facts, fmt.Sprintf("Restart count: %d", container.RestartCount))
	return facts
}

func scopesAsFacts(scopes []string) []string {
	out := make([]string, 0, len(scopes))
	for _, scope := range scopes {
		out = append(out, "Collected: "+scope)
	}
	return out
}

func sortFindings(findings []domain.IncidentFinding) []domain.IncidentFinding {
	rank := map[domain.FindingSeverity]int{
		domain.SeverityCritical: 0,
		domain.SeverityWarning:  1,
		domain.SeverityInfo:     2,
	}
	out := append([]domain.IncidentFinding(nil), findings...)
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if rank[out[j].Severity] < rank[out[i].Severity] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
