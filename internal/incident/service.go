package incident

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/resources"
)

// Service builds evidence-based incident reports.
type Service struct {
	clusters  *cluster.Manager
	resources *resources.Service
}

// NewService wires the incident analyser.
func NewService(clusters *cluster.Manager, resources *resources.Service) *Service {
	return &Service{clusters: clusters, resources: resources}
}

// Explain collects evidence and runs deterministic rules for one resource.
func (s *Service) Explain(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.IncidentReport, error) {
	obj, err := s.resources.Get(ctx, clusterID, ref)
	if err != nil {
		return domain.IncidentReport{}, err
	}

	bundle := EvidenceBundle{
		RootRef: ref,
		RootUID: string(obj.GetUID()),
	}

	switch ref.Kind {
	case domain.KindPod:
		s.collectPod(ctx, clusterID, ref, &bundle)
	default:
		if supportedWorkload(ref.Kind) {
			s.collectWorkload(ctx, clusterID, ref, &bundle)
		} else {
			bundle.noteMissing("Explain Why is not implemented for this kind yet")
		}
	}

	s.collectEvents(ctx, clusterID, ref.Namespace, ref.Name, &bundle)

	findings := Analyze(bundle)
	if supportedWorkload(ref.Kind) && ref.Kind != domain.KindPod {
		findings = append(findings, s.analyzeRelatedPods(ctx, clusterID, ref, &bundle)...)
		findings = sortFindings(findings)
	}

	return domain.IncidentReport{
		ReportID:     "report_" + uuid.NewString(),
		ClusterID:    clusterID,
		SessionEpoch: s.clusters.SessionEpoch(clusterID),
		RootRef:      ref,
		RootUID:      bundle.RootUID,
		CollectedAt:  time.Now().UTC(),
		Coverage:     coverageFrom(bundle),
		Findings:     findings,
		Issues:       bundle.Issues,
	}, nil
}

func (s *Service) collectPod(ctx context.Context, clusterID string, ref domain.ResourceRef, bundle *EvidenceBundle) {
	detail, err := s.resources.PodDetail(ctx, clusterID, ref.Namespace, ref.Name)
	if err != nil {
		bundle.noteMissing("Pod state")
		bundle.Issues = append(bundle.Issues, domain.IncidentIssue{
			Scope: "pod", Code: issueCode(err), Message: err.Error(),
		})
		return
	}
	bundle.Pod = &detail
	bundle.noteScope("Pod state")
	bundle.noteScope("Container state")
	recordContainerEvidence(ref, bundle.RootUID, detail, bundle)
}

func (s *Service) collectWorkload(ctx context.Context, clusterID string, ref domain.ResourceRef, bundle *EvidenceBundle) {
	inspect, err := s.resources.InspectResource(ctx, clusterID, ref)
	if err != nil {
		bundle.noteMissing("Workload status")
		bundle.Issues = append(bundle.Issues, domain.IncidentIssue{
			Scope: "workload", Code: issueCode(err), Message: err.Error(),
		})
	} else {
		bundle.Inspect = &inspect
		bundle.noteScope("Workload status")
	}

	groups, err := s.resources.Related(ctx, clusterID, ref)
	if err != nil {
		bundle.noteMissing("Related pods")
		bundle.Issues = append(bundle.Issues, domain.IncidentIssue{
			Scope: "related", Code: issueCode(err), Message: err.Error(),
		})
		return
	}
	bundle.Related = groups
	bundle.noteScope("Related pods")
}

func (s *Service) analyzeRelatedPods(
	ctx context.Context,
	clusterID string,
	ref domain.ResourceRef,
	bundle *EvidenceBundle,
) []domain.IncidentFinding {
	var out []domain.IncidentFinding
	for _, group := range bundle.Related {
		if group.Kind != domain.KindPod {
			continue
		}
		for _, row := range group.Rows {
			podRef := domain.ResourceRef{Kind: domain.KindPod, Namespace: row.Namespace, Name: row.Name}
			detail, err := s.resources.PodDetail(ctx, clusterID, row.Namespace, row.Name)
			if err != nil {
				continue
			}
			child := EvidenceBundle{RootRef: podRef, RootUID: row.UID, Pod: &detail}
			recordContainerEvidence(podRef, row.UID, detail, &child)
			for _, finding := range podRules(child) {
				finding.Summary = fmt.Sprintf("%s (pod %s)", finding.Summary, row.Name)
				out = append(out, finding)
			}
		}
	}
	if len(out) == 0 && len(bundle.Related) > 0 {
		_ = ref
	}
	return out
}

func (s *Service) collectEvents(ctx context.Context, clusterID, namespace, name string, bundle *EvidenceBundle) {
	events, err := s.resources.Events(ctx, clusterID, namespace, name)
	if err != nil {
		bundle.noteMissing("Events")
		bundle.Issues = append(bundle.Issues, domain.IncidentIssue{
			Scope: "events", Code: issueCode(err), Message: err.Error(),
		})
		return
	}
	bundle.Events = events
	bundle.EventsOK = true
	bundle.noteScope("Events")
	for _, event := range events {
		if event.Type != "Warning" {
			continue
		}
		bundle.addEvidence(domain.IncidentEvidence{
			ID:          "event." + event.UID,
			SourceRef:   bundle.RootRef,
			SourceUID:   bundle.RootUID,
			EventUID:    event.UID,
			FieldPath:   "event." + event.Reason,
			SafeExcerpt: truncate(event.Message, 240),
			ObservedAt:  event.LastSeen,
		})
	}
}

func recordContainerEvidence(ref domain.ResourceRef, uid string, detail domain.PodDetail, bundle *EvidenceBundle) {
	for _, container := range append(detail.Containers, detail.InitContainers...) {
		bundle.addEvidence(domain.IncidentEvidence{
			ID:        fmt.Sprintf("container.%s.state", container.Name),
			SourceRef: ref,
			SourceUID: uid,
			FieldPath: fmt.Sprintf("container.%s.state", container.Name),
			SafeExcerpt: fmt.Sprintf("state=%s restarts=%d lastTermination=%s exit=%d",
				container.State, container.RestartCount, container.LastTerminationReason, container.LastExitCode),
			ObservedAt: time.Now().UTC(),
		})
	}
}

func supportedWorkload(kind domain.Kind) bool {
	switch kind {
	case domain.KindPod, domain.KindDeployment, domain.KindStatefulSet, domain.KindDaemonSet,
		domain.KindJob, domain.KindPersistentVolumeClaim:
		return true
	default:
		return false
	}
}

func issueCode(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "forbidden"):
		return "forbidden"
	case strings.Contains(msg, "unauthorized"):
		return "unauthorized"
	default:
		return "unavailable"
	}
}

func truncate(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	return text[:max-1] + "…"
}
