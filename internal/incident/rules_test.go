package incident

import (
	"testing"

	"biebie-kube/internal/domain"
)

func TestCrashLoopBackOffFinding(t *testing.T) {
	detail := domain.PodDetail{
		Ref:    domain.ResourceRef{Kind: domain.KindPod, Namespace: "team-a", Name: "demo"},
		Status: "Pending",
		Containers: []domain.ContainerInfo{{
			Name:         "app",
			State:        "CrashLoopBackOff",
			RestartCount: 5,
		}},
	}
	bundle := EvidenceBundle{RootRef: detail.Ref, Pod: &detail}
	recordContainerEvidence(detail.Ref, "uid-demo", detail, &bundle)

	findings := podRules(bundle)
	if len(findings) != 1 || findings[0].RuleID != "pod.crash_loop_backoff" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestOOMKilledFindingAfterRestart(t *testing.T) {
	detail := domain.PodDetail{
		Ref:    domain.ResourceRef{Kind: domain.KindPod, Namespace: "team-a", Name: "demo"},
		Status: "Running",
		Containers: []domain.ContainerInfo{{
			Name:                  "app",
			State:                 "Running",
			Ready:                 true,
			RestartCount:          2,
			LastTerminationReason: "OOMKilled",
			LastExitCode:          137,
		}},
	}
	bundle := EvidenceBundle{RootRef: detail.Ref, Pod: &detail}
	recordContainerEvidence(detail.Ref, "uid-demo", detail, &bundle)

	findings := podRules(bundle)
	if len(findings) != 1 || findings[0].RuleID != "pod.oom_killed" {
		t.Fatalf("findings = %+v", findings)
	}
}

func TestSucceededPodProducesNoFinding(t *testing.T) {
	bundle := EvidenceBundle{
		Pod: &domain.PodDetail{Status: "Succeeded", Health: domain.HealthHealthy},
	}
	if len(podRules(bundle)) != 0 {
		t.Fatal("succeeded pod must not produce incident findings")
	}
}

func TestInsufficientEvidenceWhenMissingScopes(t *testing.T) {
	bundle := EvidenceBundle{
		Scopes:  []string{"Pod state"},
		Missing: []string{"Events"},
	}
	findings := Analyze(bundle)
	if len(findings) != 1 || findings[0].ConfidenceClass != domain.IncidentConfidenceUnknown {
		t.Fatalf("findings = %+v", findings)
	}
}
