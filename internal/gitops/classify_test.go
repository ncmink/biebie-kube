package gitops

import (
	"strings"
	"testing"

	"biebie-kube/internal/argocd"
	"biebie-kube/internal/domain"
)

func TestAControllersOwnFieldIsSetAsideWithAReason(t *testing.T) {
	// These genuinely exist only in the cluster, so normalisation cannot
	// remove them without lying about the object. They are kept, explained,
	// and put behind the meaningful ones.
	header := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: ak-super-auto}\n"

	for name, test := range map[string]struct {
		live string
		path string
	}{
		"deployment revision": {
			live: header + `metadata:
  name: ak-super-auto
  annotations: {deployment.kubernetes.io/revision: "22"}
`,
			path: "metadata.annotations.deployment.kubernetes.io/revision",
		},
		"argo tracking label": {
			live: header + `metadata:
  name: ak-super-auto
  labels: {argocd.argoproj.io/instance: ak-super-auto}
`,
			path: "metadata.labels.argocd.argoproj.io/instance",
		},
		"argo default tracking label": {
			live: header + "metadata:\n  name: ak-super-auto\n  labels: {" +
				argocd.InstanceLabel + ": ak-super-auto}\n",
			path: "metadata.labels." + argocd.InstanceLabel,
		},
		"rollout restart": {
			live: header + `spec:
  template:
    metadata:
      annotations: {kubectl.kubernetes.io/restartedAt: "2026-02-01T09:14:00Z"}
`,
			path: "spec.template.metadata.annotations.kubectl.kubernetes.io/restartedAt",
		},
	} {
		t.Run(name, func(t *testing.T) {
			found := classify(differencesBetween(t, header, object(t, test.live), false))
			if len(found) != 1 {
				t.Fatalf("reported %+v", found)
			}
			if found[0].Path != test.path {
				t.Fatalf("path = %q, want %q", found[0].Path, test.path)
			}
			if found[0].Class != domain.DifferenceSystemManaged {
				t.Fatalf("class = %q", found[0].Class)
			}
			if found[0].Reason == "" {
				t.Fatal("a field set aside with no reason is a field the reader has to take on trust")
			}
		})
	}
}

func TestAnAnnotationArrivesOneKeyAtATimeRatherThanAsTheWholeBlock(t *testing.T) {
	// The report showed `metadata.annotations` as one row holding a JSON blob,
	// which is unreadable and, worse, uncategorisable: the block cannot be set
	// aside without setting aside every annotation in it.
	header := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: ak-super-auto}\n"
	live := object(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ak-super-auto
  annotations:
    deployment.kubernetes.io/revision: "22"
    team: payments
`)

	found := classify(differencesBetween(t, header, live, false))
	if len(found) != 2 {
		t.Fatalf("reported %+v", found)
	}

	classes := map[string]domain.DifferenceClass{}
	for _, difference := range found {
		classes[difference.Path] = difference.Class
	}
	if classes["metadata.annotations.deployment.kubernetes.io/revision"] != domain.DifferenceSystemManaged {
		t.Fatalf("the controller's annotation was not set aside: %+v", found)
	}
	// An annotation somebody wrote is not the controller's, and sits in front
	// of the reader with the rest of the meaningful differences.
	if classes["metadata.annotations.team"] != domain.DifferenceMeaningful {
		t.Fatalf("somebody's own annotation was set aside: %+v", found)
	}
}

func TestAFieldNobodyRecognisedStaysInFrontOfTheReader(t *testing.T) {
	// The rule the whole classifier rests on. Being unable to account for a
	// difference is a reason to show it, not a reason to hide it: showing one
	// field too many costs a moment and hiding one costs a drift nobody sees.
	header := "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: payment-api}\nspec: {paused: false}\n"
	live := object(t, "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: payment-api}\n"+
		"spec: {paused: false, minReadySeconds: 45}\n")

	found := classify(differencesBetween(t, header, live, false))
	if len(found) != 1 {
		t.Fatalf("reported %+v", found)
	}
	if found[0].Class != domain.DifferenceMeaningful {
		t.Fatalf("an unrecognised field was hidden as %q", found[0].Class)
	}
}

func TestAFieldBothSidesDeclareIsNeverSetAside(t *testing.T) {
	// Only a field the cluster has and the source does not can have been
	// written by a controller. A revision annotation the repository also
	// carries is a disagreement between two people who both wrote one down.
	header := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: ak-super-auto\n" +
		"  annotations: {deployment.kubernetes.io/revision: \"1\"}\n"
	live := object(t, "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: ak-super-auto\n"+
		"  annotations: {deployment.kubernetes.io/revision: \"22\"}\n")

	found := classify(differencesBetween(t, header, live, false))
	if len(found) != 1 || found[0].Class != domain.DifferenceMeaningful {
		t.Fatalf("reported %+v", found)
	}
}

func TestADifferenceCarriesTheWordsAPersonWouldUse(t *testing.T) {
	// Worked out in Go rather than in the panel, because deciding that
	// `spec.template.spec.containers[name=api].image` is a container's image
	// is Kubernetes knowledge, and Kubernetes knowledge in a Vue component is
	// Kubernetes knowledge nobody can test.
	for path, want := range map[string][2]string{
		"spec.template.spec.containers[name=api].image":                   {"Container image", "api"},
		"spec.template.spec.containers[name=api]":                         {"Container", "api"},
		"spec.template.spec.containers[name=api].resources.limits.memory": {"Memory limit", "api"},
		"spec.template.spec.containers[name=api].resources.requests.cpu":  {"CPU request", "api"},
		"spec.template.spec.containers[name=api].env[name=DB_HOST].value": {"Environment variable", "DB_HOST"},
		"spec.replicas":                    {"Replicas", ""},
		"spec.strategy.type":               {"Strategy", ""},
		"spec.template.spec.schedulerName": {"Scheduler", ""},
		"metadata.annotations.deployment.kubernetes.io/revision":            {"Annotation", "deployment.kubernetes.io/revision"},
		"metadata.labels.argocd.argoproj.io/instance":                       {"Label", "argocd.argoproj.io/instance"},
		"spec.template.spec.affinity.nodeAffinity.requiredDuringScheduling": {"", ""},
	} {
		label, subject := describe(path)
		if label != want[0] || subject != want[1] {
			t.Fatalf("%s = (%q, %q), want (%q, %q)", path, label, subject, want[0], want[1])
		}
	}
}

func TestSecretValuesStayRedactedThroughClassification(t *testing.T) {
	// Classification adds labels and reasons to differences, and a label is
	// another string that could carry a value out. The key names are the
	// useful half and the values do not leave the process, however the
	// difference is dressed up afterwards.
	source := `
apiVersion: v1
kind: Secret
metadata: {name: api-credentials}
data: {DB_PASSWORD: ZGVzaXJlZA==}
`
	live := object(t, `
apiVersion: v1
kind: Secret
metadata: {name: api-credentials}
data: {DB_PASSWORD: bGl2ZQ==, API_TOKEN: c2Vjb25k}
`)

	found := classify(differencesBetween(t, source, live, true))
	if len(found) != 2 {
		t.Fatalf("reported %+v", found)
	}
	for _, difference := range found {
		if !difference.Redacted {
			t.Fatalf("%s was not redacted", difference.Path)
		}
		everything := strings.Join([]string{
			difference.Source, difference.Live, difference.Label, difference.Subject, difference.Reason,
		}, " ")
		if strings.Contains(everything, "ZGVzaXJlZA") || strings.Contains(everything, "bGl2ZQ") {
			t.Fatalf("a secret value crossed the boundary: %+v", difference)
		}
	}
}
