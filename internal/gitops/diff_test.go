package gitops

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/argocd"
	"biebie-kube/internal/domain"
)

func TestAnObjectThatMatchesItsManifestHasNoDifferences(t *testing.T) {
	manifest := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-api
  namespace: payment
spec:
  replicas: 3
  template:
    spec:
      containers:
        - name: api
          image: api:v1.8
`
	// The live object as the API server returns it: the same thing, wrapped in
	// the bookkeeping Kubernetes keeps about it.
	live := object(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-api
  namespace: payment
  uid: 7c2b1f10-0000-4000-8000-000000000000
  resourceVersion: "884213"
  generation: 4
  creationTimestamp: "2026-02-01T09:14:00Z"
  selfLink: /apis/apps/v1/namespaces/payment/deployments/payment-api
  managedFields:
    - manager: argocd-controller
      operation: Apply
spec:
  replicas: 3
  template:
    metadata:
      creationTimestamp: null
    spec:
      containers:
        - name: api
          image: api:v1.8
status:
  readyReplicas: 3
  observedGeneration: 4
`)

	if found := differencesBetween(t, manifest, live, false); len(found) != 0 {
		t.Fatalf("an object matching its manifest reported %d differences: %+v", len(found), found)
	}
}

func TestAnIntegerFromYamlEqualsTheSameIntegerFromTheApiServer(t *testing.T) {
	// The trap this whole comparison lives or dies on. A number read from YAML
	// arrives as a float64 and the same number from the dynamic client arrives
	// as an int64, so without one decoder for both sides every replica count,
	// every port and every timeout in every repository would report as drift.
	source, err := canonicalManifest([]byte("spec:\n  replicas: 3\n  ports: [8080]\n"))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	live, err := canonicalObject(map[string]any{
		"spec": map[string]any{"replicas": int64(3), "ports": []any{int64(8080)}},
	})
	if err != nil {
		t.Fatalf("object: %v", err)
	}

	if found := compare(source, live, false); len(found) != 0 {
		t.Fatalf("the same numbers reported as different: %+v", found)
	}
}

func TestARuntimeFieldIsNotDrift(t *testing.T) {
	// Each of these is written by Kubernetes rather than by a person, and a
	// panel that reported them would say "differences" about every object in
	// every cluster.
	for name, live := range map[string]string{
		"resourceVersion":   `metadata: {name: payment-api, resourceVersion: "99123"}`,
		"uid":               `metadata: {name: payment-api, uid: 7c2b1f10-0000-4000-8000-000000000000}`,
		"generation":        `metadata: {name: payment-api, generation: 12}`,
		"creationTimestamp": `metadata: {name: payment-api, creationTimestamp: "2026-02-01T09:14:00Z"}`,
		"selfLink":          `metadata: {name: payment-api, selfLink: /apis/apps/v1/x}`,
		"managedFields":     `metadata: {name: payment-api, managedFields: [{manager: kubectl}]}`,
		"status":            `metadata: {name: payment-api}` + "\nstatus: {readyReplicas: 3}",
		"last applied":      `metadata: {name: payment-api, annotations: {"kubectl.kubernetes.io/last-applied-configuration": "{}"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			found := differencesBetween(t, "metadata:\n  name: payment-api\n", object(t, live), false)
			if len(found) != 0 {
				t.Fatalf("reported %+v", found)
			}
		})
	}
}

func TestArgoCdsOwnTrackingAnnotationIsNotDrift(t *testing.T) {
	// Argo CD writes it onto the object after applying, so it is in the
	// cluster and never in the repository. Left in, every annotation-tracked
	// object in the cluster would report one difference for ever.
	live := object(t, `
metadata:
  name: payment-api
  annotations:
    `+argocd.TrackingAnnotation+`: "payment-api:apps/Deployment:payment/payment-api"
`)
	if found := differencesBetween(t, "metadata:\n  name: payment-api\n", live, false); len(found) != 0 {
		t.Fatalf("reported %+v", found)
	}
}

func TestAManifestWithNoNamespaceDoesNotReportTheNamespaceAsDrift(t *testing.T) {
	// A manifest that names no namespace is placed in the one the Application
	// sends it to. Most manifests are written this way.
	live := object(t, `metadata: {name: payment-api, namespace: payment}`)
	if found := differencesBetween(t, "metadata:\n  name: payment-api\n", live, false); len(found) != 0 {
		t.Fatalf("reported %+v", found)
	}
}

func TestAManifestThatNamesADifferentNamespaceStillReportsIt(t *testing.T) {
	// Suppressing the namespace when the manifest is silent is not the same as
	// suppressing it when the manifest disagrees.
	live := object(t, `metadata: {name: payment-api, namespace: payment}`)
	found := differencesBetween(t, "metadata:\n  name: payment-api\n  namespace: payment-staging\n", live, false)
	if len(found) != 1 || found[0].Path != "metadata.namespace" {
		t.Fatalf("reported %+v", found)
	}
}

func TestAChangedReplicaCountIsReported(t *testing.T) {
	live := object(t, `metadata: {name: payment-api}`+"\nspec: {replicas: 3}")
	found := differencesBetween(t, "metadata:\n  name: payment-api\nspec:\n  replicas: 5\n", live, false)

	if len(found) != 1 {
		t.Fatalf("reported %+v", found)
	}
	if found[0].Path != "spec.replicas" || found[0].Kind != domain.DifferenceChanged {
		t.Fatalf("difference = %+v", found[0])
	}
	if found[0].Source != "5" || found[0].Live != "3" {
		t.Fatalf("source = %q, live = %q", found[0].Source, found[0].Live)
	}
}

func TestAChangedContainerImageIsNamedByContainerRatherThanByPosition(t *testing.T) {
	manifest := `
metadata: {name: payment-api}
spec:
  template:
    spec:
      containers:
        - {name: api, image: "api:v1.9"}
`
	live := object(t, `
metadata: {name: payment-api}
spec:
  template:
    spec:
      containers:
        - {name: api, image: "api:v1.8"}
`)
	found := differencesBetween(t, manifest, live, false)
	if len(found) != 1 {
		t.Fatalf("reported %+v", found)
	}
	if found[0].Path != "spec.template.spec.containers[name=api].image" {
		t.Fatalf("path = %q", found[0].Path)
	}
}

func TestReorderingContainersIsNotADifference(t *testing.T) {
	// This is the requirement that decides whether the feature is trusted. A
	// sidecar moving to the front of the list must not report every field of
	// both containers as changed, because a reader who sees that once stops
	// believing the panel.
	manifest := `
metadata: {name: payment-api}
spec:
  template:
    spec:
      containers:
        - {name: api, image: "api:v1.8"}
        - {name: sidecar, image: "proxy:v2"}
`
	live := object(t, `
metadata: {name: payment-api}
spec:
  template:
    spec:
      containers:
        - {name: sidecar, image: "proxy:v2"}
        - {name: api, image: "api:v1.8"}
`)
	if found := differencesBetween(t, manifest, live, false); len(found) != 0 {
		t.Fatalf("reordering reported %d differences: %+v", len(found), found)
	}
}

func TestAContainerOnlyInOneSideIsReportedAsThatContainer(t *testing.T) {
	manifest := `
metadata: {name: payment-api}
spec:
  template:
    spec:
      containers:
        - {name: api, image: "api:v1.8"}
`
	live := object(t, `
metadata: {name: payment-api}
spec:
  template:
    spec:
      containers:
        - {name: api, image: "api:v1.8"}
        - {name: injected-proxy, image: "istio:1.20"}
`)
	found := differencesBetween(t, manifest, live, false)
	if len(found) != 1 {
		t.Fatalf("reported %+v", found)
	}
	if found[0].Path != "spec.template.spec.containers[name=injected-proxy]" {
		t.Fatalf("path = %q", found[0].Path)
	}
	// A sidecar a mesh injected is not drift somebody caused, and the kind is
	// what lets the panel say so without claiming to know which it is.
	if found[0].Kind != domain.DifferenceAddedInLive {
		t.Fatalf("kind = %q", found[0].Kind)
	}
}

func TestAnOrderedListIsStillComparedByPosition(t *testing.T) {
	// `command` is order-sensitive and has no key. Matching it by name would
	// be inventing semantics Kubernetes does not have.
	manifest := `
metadata: {name: payment-api}
spec:
  template:
    spec:
      containers:
        - name: api
          command: ["/bin/api", "--serve"]
`
	live := object(t, `
metadata: {name: payment-api}
spec:
  template:
    spec:
      containers:
        - name: api
          command: ["--serve", "/bin/api"]
`)
	found := differencesBetween(t, manifest, live, false)
	if len(found) != 2 {
		t.Fatalf("reported %+v", found)
	}
	if !strings.Contains(found[0].Path, "command[0]") {
		t.Fatalf("path = %q", found[0].Path)
	}
}

func TestAFieldOnlyInLiveIsReportedSeparatelyFromAChange(t *testing.T) {
	// Server defaulting looks exactly like this, and so does a field somebody
	// added by hand. The panel is not told which, and neither is this test.
	live := object(t, `
metadata: {name: payment-api}
spec:
  replicas: 3
  progressDeadlineSeconds: 600
`)
	found := differencesBetween(t, "metadata:\n  name: payment-api\nspec:\n  replicas: 3\n", live, false)
	if len(found) != 1 {
		t.Fatalf("reported %+v", found)
	}
	if found[0].Path != "spec.progressDeadlineSeconds" || found[0].Kind != domain.DifferenceAddedInLive {
		t.Fatalf("difference = %+v", found[0])
	}
	if found[0].Source != "" || found[0].Live != "600" {
		t.Fatalf("source = %q, live = %q", found[0].Source, found[0].Live)
	}
}

func TestAFieldOnlyInTheManifestIsReported(t *testing.T) {
	// The rarer and more interesting direction: it can mean the manifest was
	// never applied.
	live := object(t, `metadata: {name: payment-api}`+"\nspec: {replicas: 3}")
	found := differencesBetween(t,
		"metadata:\n  name: payment-api\nspec:\n  replicas: 3\n  revisionHistoryLimit: 2\n", live, false)

	if len(found) != 1 {
		t.Fatalf("reported %+v", found)
	}
	if found[0].Path != "spec.revisionHistoryLimit" || found[0].Kind != domain.DifferenceMissingInLive {
		t.Fatalf("difference = %+v", found[0])
	}
}

func TestADeletionTimestampIsReportedRatherThanSwallowed(t *testing.T) {
	// It will never be in a manifest, which is an argument for ignoring it and
	// a better argument for not ignoring it: an object being deleted is not an
	// object that matches its manifest.
	live := object(t, `metadata: {name: payment-api, deletionTimestamp: "2026-02-01T09:14:00Z"}`)
	found := differencesBetween(t, "metadata:\n  name: payment-api\n", live, false)
	if len(found) != 1 || found[0].Path != "metadata.deletionTimestamp" {
		t.Fatalf("reported %+v", found)
	}
}

func TestASecretsValuesNeverReachTheDifference(t *testing.T) {
	// The one test in this file whose failure would be a leak rather than a
	// wrong answer. The key names are useful and the values are not this
	// panel's to hand out.
	manifest := `
apiVersion: v1
kind: Secret
metadata: {name: api-credentials}
data:
  DB_PASSWORD: aHVudGVyMi1kZXNpcmVk
  API_TOKEN: Z2hwX2Rlc2lyZWQ=
`
	live := object(t, `
apiVersion: v1
kind: Secret
metadata: {name: api-credentials}
data:
  DB_PASSWORD: aHVudGVyMi1saXZl
`)
	found := differencesBetween(t, manifest, live, true)
	if len(found) != 2 {
		t.Fatalf("reported %+v", found)
	}
	for _, difference := range found {
		if !difference.Redacted {
			t.Fatalf("%s was not redacted", difference.Path)
		}
		if difference.Source != "" || difference.Live != "" {
			t.Fatalf("a secret value crossed the boundary: %+v", difference)
		}
	}
	// The key is the useful half and is not itself secret: it is already on
	// the resource list as a key count and in the inspector as a name.
	if found[0].Path != "data.API_TOKEN" {
		t.Fatalf("path = %q", found[0].Path)
	}
}

func TestAWholeSecretDataMapIsRedactedToo(t *testing.T) {
	// The subtle version of the leak: when the map is missing on one side, the
	// difference is the whole map rendered as one value.
	manifest := `
apiVersion: v1
kind: Secret
metadata: {name: api-credentials}
data:
  DB_PASSWORD: aHVudGVyMg==
`
	live := object(t, `
apiVersion: v1
kind: Secret
metadata: {name: api-credentials}
`)
	found := differencesBetween(t, manifest, live, true)
	if len(found) != 1 {
		t.Fatalf("reported %+v", found)
	}
	if !found[0].Redacted || found[0].Source != "" {
		t.Fatalf("the whole data map leaked: %+v", found[0])
	}
}

func TestANonSecretKindKeepsItsValues(t *testing.T) {
	// Redaction follows the catalogue's Sensitive flag. A ConfigMap named
	// `data` is not a secret and blanking it would make the panel useless for
	// the kind people compare most often after Deployments.
	manifest := `
apiVersion: v1
kind: ConfigMap
metadata: {name: payment-config}
data:
  LOG_LEVEL: debug
`
	live := object(t, `
apiVersion: v1
kind: ConfigMap
metadata: {name: payment-config}
data:
  LOG_LEVEL: info
`)
	found := differencesBetween(t, manifest, live, false)
	if len(found) != 1 || found[0].Source != "debug" || found[0].Live != "info" {
		t.Fatalf("reported %+v", found)
	}
}

func TestALongValueIsShortenedRatherThanSentWhole(t *testing.T) {
	long := strings.Repeat("x", valueLimit*3)
	live := object(t, `metadata: {name: payment-api}`+"\ndata: {blob: short}")
	found := differencesBetween(t, "metadata:\n  name: payment-api\ndata:\n  blob: "+long+"\n", live, false)

	if len(found) != 1 {
		t.Fatalf("reported %+v", found)
	}
	if len(found[0].Source) > valueLimit+len("…") {
		t.Fatalf("a value of %d characters crossed the binding", len(found[0].Source))
	}
}

func TestAnObjectCompletelyUnlikeItsManifestDoesNotReportForEver(t *testing.T) {
	// A name matched by coincidence, or a repository half-migrated. The list
	// is capped because four hundred rows is a list nobody scrolls.
	var manifest strings.Builder
	manifest.WriteString("metadata:\n  name: payment-api\ndata:\n")
	for index := range differenceLimit * 2 {
		manifest.WriteString("  key")
		manifest.WriteString(strings.Repeat("0", 4-len(itoa(index))))
		manifest.WriteString(itoa(index))
		manifest.WriteString(": value\n")
	}

	live := object(t, `metadata: {name: payment-api}`)
	found := differencesBetween(t, manifest.String(), live, false)
	if len(found) > differenceLimit {
		t.Fatalf("reported %d differences", len(found))
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// differencesBetween runs a manifest and a live object through the same path
// the service uses, so the tests exercise normalisation and comparison
// together rather than a diff of things nothing would ever produce.
func differencesBetween(t *testing.T, manifest string, live *unstructured.Unstructured, secret bool) []domain.StateDifference {
	t.Helper()

	source, err := canonicalManifest([]byte(manifest))
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	current, err := canonicalObject(live.Object)
	if err != nil {
		t.Fatalf("object: %v", err)
	}

	normalise(current, source)
	return compare(source, current, secret)
}

func object(t *testing.T, document string) *unstructured.Unstructured {
	t.Helper()

	// Decoded the way the dynamic client decodes, so integers arrive as int64
	// exactly as they would from a real API server. A test that built the map
	// by hand would quietly test something easier.
	decoded, err := canonicalManifest([]byte(document))
	if err != nil {
		t.Fatalf("live object: %v", err)
	}
	return &unstructured.Unstructured{Object: decoded}
}
