package incident

import (
	"strings"
	"testing"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/testfixture"
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

func TestNodeNetworkIncidentFiresHeuristic(t *testing.T) {
	findings := Analyze(networkIncident("weave-net-abc"))
	if !hasRule(findings, "node.network_plugin_not_ready") {
		t.Fatalf("network plugin finding missing: %+v", ruleIDs(findings))
	}
	if !hasRule(findings, "node.network_resource_exhaustion") {
		t.Fatalf("heuristic missing: %+v", ruleIDs(findings))
	}
	sawNonCritical := false
	for _, finding := range findings {
		if finding.Severity != domain.SeverityCritical {
			sawNonCritical = true
			continue
		}
		if sawNonCritical {
			t.Fatalf("critical finding %s sorted after a lower severity", finding.RuleID)
		}
	}
	heuristic := findingByID(findings, "node.network_resource_exhaustion")
	if heuristic.ConfidenceClass != domain.IncidentConfidenceSupported {
		t.Fatalf("confidence = %s", heuristic.ConfidenceClass)
	}
	if strings.Contains(heuristic.Summary, "weave") || strings.Contains(heuristic.Explanation, "weave") {
		t.Fatal("the heuristic names a network plugin in its conclusion")
	}
}

func TestNodeNetworkRulesIgnoreAgentName(t *testing.T) {
	weave := ruleIDs(Analyze(networkIncident("weave-net-abc")))
	calico := ruleIDs(Analyze(networkIncident("calico-node-xyz")))
	if len(weave) != len(calico) {
		t.Fatalf("weave %v calico %v", weave, calico)
	}
	for id := range weave {
		if _, ok := calico[id]; !ok {
			t.Fatalf("calico bundle missing %s", id)
		}
	}
	plugin := findingByID(Analyze(networkIncident("calico-node-xyz")), "node.network_plugin_not_ready")
	joined := strings.Join(plugin.ObservedFacts, "\n")
	if !strings.Contains(joined, "calico-node-xyz") {
		t.Fatalf("agent name was not quoted as a fact: %s", joined)
	}
	if strings.Contains(plugin.Explanation, "calico") {
		t.Fatal("explanation treats the agent name as the cause")
	}
}

func TestCrowdedHealthyNodeHasNoNetworkFinding(t *testing.T) {
	node := domain.NodeDetail{
		Name:    "worker",
		Ready:   true,
		MaxPods: 110,
		Conditions: []domain.Condition{
			{Type: "Ready", Status: "True", Reason: "KubeletReady"},
			{Type: "MemoryPressure", Status: "False"},
		},
	}
	bundle := EvidenceBundle{
		Node: &node,
		NodePods: &domain.NodePods{
			PodsUsed: 105,
			MaxPods:  110,
		},
	}
	for _, finding := range Analyze(bundle) {
		if strings.HasPrefix(finding.RuleID, "node.network") || finding.RuleID == "event.pod_sandbox_failed" {
			t.Fatalf("healthy node produced %s", finding.RuleID)
		}
	}
}

func TestAllocationErrorUnderMemoryPressureSkipsHeuristic(t *testing.T) {
	bundle := networkIncident("agent")
	for i := range bundle.Node.Conditions {
		if bundle.Node.Conditions[i].Type == "MemoryPressure" {
			bundle.Node.Conditions[i].Status = "True"
		}
	}
	if hasRule(Analyze(bundle), "node.network_resource_exhaustion") {
		t.Fatal("memory pressure already explains the allocation failure")
	}
}

func TestForbiddenNodeListsStayPartial(t *testing.T) {
	node := testfixture.NodeNetworkNotReady("worker")
	bundle := EvidenceBundle{
		Node:    &node,
		Missing: []string{"Pods on node", "Events"},
		Issues: []domain.IncidentIssue{{
			Scope: "pods", Code: "forbidden", Message: "pods is forbidden",
		}},
	}
	coverage := coverageFrom(bundle)
	if coverage.State != domain.CoveragePartial {
		t.Fatalf("coverage = %s", coverage.State)
	}
	findings := Analyze(bundle)
	if !hasRule(findings, "node.not_ready") {
		t.Fatalf("condition finding missing: %+v", findings)
	}
}

func TestSandboxFailureWithoutAllocationSkipsHeuristic(t *testing.T) {
	bundle := EvidenceBundle{
		PodEvents: []domain.EventRow{{
			UID:     "sandbox-plain",
			Type:    "Warning",
			Reason:  "FailedCreatePodSandBox",
			Message: "failed to create pod sandbox: address already in use",
		}},
	}
	findings := Analyze(bundle)
	if !hasRule(findings, "event.pod_sandbox_failed") {
		t.Fatalf("sandbox finding missing: %+v", findings)
	}
	if hasRule(findings, "node.network_resource_exhaustion") {
		t.Fatal("allocation heuristic fired without an allocation error")
	}
	sandbox := findingByID(findings, "event.pod_sandbox_failed")
	if strings.Contains(strings.ToLower(sandbox.Explanation), "network setup failed") {
		t.Fatal("a sandbox error that does not mention networking was described as one")
	}
}

func networkIncident(agent string) EvidenceBundle {
	node := testfixture.NodeNetworkNotReady("worker")
	pods := testfixture.NodePodsWithUnreadyAgent(agent, 40, 110)
	return EvidenceBundle{
		Node:      &node,
		NodePods:  &pods,
		PodEvents: []domain.EventRow{testfixture.SandboxAllocationEvent()},
	}
}

func hasRule(findings []domain.IncidentFinding, id string) bool {
	for _, finding := range findings {
		if finding.RuleID == id {
			return true
		}
	}
	return false
}

func findingByID(findings []domain.IncidentFinding, id string) domain.IncidentFinding {
	for _, finding := range findings {
		if finding.RuleID == id {
			return finding
		}
	}
	return domain.IncidentFinding{}
}

func ruleIDs(findings []domain.IncidentFinding) map[string]int {
	out := map[string]int{}
	for _, finding := range findings {
		out[finding.RuleID]++
	}
	return out
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
