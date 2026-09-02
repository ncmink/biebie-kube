package manifest

import (
	"context"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// The snapshot and the guards around writing it back.

func TestTheSnapshotDropsWhatTheClusterWritesAndKeepsWhatWasDeclared(t *testing.T) {
	// Server-managed fields make a manifest unreadable and are rejected or
	// ignored on the way back, so showing them would be showing text that
	// cannot be applied.
	live := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":              "super-report",
			"namespace":         "reporting",
			"resourceVersion":   "88421",
			"uid":               "6f0d1c8a-1111-2222-3333-444455556666",
			"generation":        int64(7),
			"creationTimestamp": "2026-01-04T09:15:00Z",
			"managedFields":     []any{map[string]any{"manager": "kube-controller-manager"}},
			"labels":            map[string]any{"app": "super-report"},
		},
		"spec":   map[string]any{"replicas": int64(3)},
		"status": map[string]any{"readyReplicas": int64(3)},
	}}

	snapshot, err := render(live)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, dropped := range []string{"managedFields", "resourceVersion", "uid", "generation", "creationTimestamp", "status:"} {
		if strings.Contains(snapshot, dropped) {
			t.Errorf("the snapshot still carries %s", dropped)
		}
	}
	for _, kept := range []string{"name: super-report", "namespace: reporting", "replicas: 3", "app: super-report"} {
		if !strings.Contains(snapshot, kept) {
			t.Errorf("the snapshot lost %q", kept)
		}
	}
}

func TestTheVersionIsHeldOnTheSessionRatherThanInTheText(t *testing.T) {
	// The concurrency token lives on the session because a resourceVersion in
	// an editable manifest is a line somebody deletes while tidying up, and
	// the protection would leave with it.
	live := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1",
		"kind":       "ConfigMap",
		"metadata":   map[string]any{"name": "settings", "namespace": "reporting", "resourceVersion": "88421"},
	}}

	snapshot, err := render(live)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(snapshot, "88421") {
		t.Fatal("the version is in the editable text, where it can be deleted by accident")
	}
	if live.GetResourceVersion() != "88421" {
		t.Fatal("rendering mutated the live object it was given")
	}
}

func TestAWriteAgainstAMovedObjectIsRefused(t *testing.T) {
	err := guard("88421", "88999")

	if err == nil {
		t.Fatal("a write against a version that no longer exists was allowed through")
	}
	if !strings.Contains(err.Error(), "nothing was written") {
		t.Fatalf("the refusal does not say the write did not happen: %v", err)
	}
}

func TestAWriteWithNoVersionIsRefusedRatherThanStamped(t *testing.T) {
	// Taking the live version when none was supplied is what the code did
	// before, and it made every write succeed while looking protected.
	if err := guard("", "88421"); err == nil {
		t.Fatal("a versionless write was accepted")
	}
}

func TestAWriteAtTheVersionTheEditorOpenedAtGoesAhead(t *testing.T) {
	if err := guard("88421", "88421"); err != nil {
		t.Fatalf("an unchanged object refused a write: %v", err)
	}
}

func TestOwnershipIsCheckedBeforeAnythingIsRead(t *testing.T) {
	// Ownership is the first guard in Apply, ahead of the kind lookup and the
	// cluster read. A service with no way to ask therefore refuses without
	// touching a cluster — which is also what proves the refusal is not the
	// frontend's to skip.
	service := &Service{}

	_, err := service.Apply(context.Background(), "staging",
		domain.ResourceRef{Kind: domain.KindDeployment, Namespace: "reporting", Name: "super-report"},
		"apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: super-report\n", "88421")

	if err == nil {
		t.Fatal("a write went ahead with ownership unestablished")
	}
	if !strings.Contains(err.Error(), "nothing was applied") {
		t.Fatalf("the refusal does not say the write did not happen: %v", err)
	}
}

func TestAnUnestablishedOwnershipClosesTheGateWithoutFailing(t *testing.T) {
	// The editor still opens under a failed ownership check. Reading YAML is
	// not a mutation, and an Argo CD permission this account lacks is not a
	// reason to take the inspector away.
	gate := (&Service{}).gate(context.Background(), "staging",
		domain.ResourceRef{Kind: domain.KindDeployment, Namespace: "reporting", Name: "super-report"})

	if gate.Allowed {
		t.Fatal("ownership could not be checked and the write was offered anyway")
	}
	if gate.Status != domain.OwnershipStatusUnknown {
		t.Fatalf("status = %q", gate.Status)
	}
	if gate.Managed {
		t.Fatal("an unknown answer reports as managed, sending somebody to a repository that may not exist")
	}
	if strings.TrimSpace(gate.Reason) == "" {
		t.Fatal("the gate is closed with nothing to show the person who wanted to write")
	}
}

func TestFreshnessSeparatesUnchangedFromUncheckable(t *testing.T) {
	// "It has not changed" and "we could not tell" must not look the same to
	// somebody about to press Apply.
	out, err := (&Service{}).Freshness(context.Background(), "staging",
		domain.ResourceRef{Kind: domain.KindDeployment, Namespace: "reporting", Name: "super-report"}, "")
	if err != nil {
		t.Fatalf("freshness: %v", err)
	}

	if out.Stale {
		t.Fatal("an unanswerable question was answered as stale")
	}
	if out.Unchecked == "" {
		t.Fatal("a session with no version to compare reported as fresh")
	}
}
