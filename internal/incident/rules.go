package incident

import (
	"fmt"

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
				ObservedFacts: factsForTermination(container),
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

func eventRules(bundle EvidenceBundle) []domain.IncidentFinding {
	if !bundle.EventsOK {
		return nil
	}
	var out []domain.IncidentFinding
	for _, event := range bundle.Events {
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
		domain.SeverityCritical:      0,
		domain.SeverityWarning:       1,
		domain.SeverityInfo:          2,
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
