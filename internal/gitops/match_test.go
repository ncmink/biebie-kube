package gitops

import (
	"strings"
	"testing"
)

var deployment = identity{group: "apps", kind: "Deployment", namespace: "payment", name: "payment-api"}

func TestTheDocumentThatDeclaresTheObjectIsFound(t *testing.T) {
	file := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-api
  namespace: payment
spec:
  replicas: 3
`
	found := documents([]byte(file), deployment)
	if len(found) != 1 {
		t.Fatalf("found %d documents", len(found))
	}
	if found[0].document != 0 {
		t.Fatalf("document = %d", found[0].document)
	}
	if !strings.Contains(found[0].content, "replicas: 3") {
		t.Fatalf("the content is not the document: %q", found[0].content)
	}
}

func TestTheRightPartOfAMultiDocumentFileIsNamed(t *testing.T) {
	// Naming the file without naming the part of it is an answer that still
	// leaves somebody reading.
	file := `apiVersion: v1
kind: ConfigMap
metadata:
  name: payment-api
  namespace: payment
---
apiVersion: v1
kind: Service
metadata:
  name: payment-api
  namespace: payment
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-api
  namespace: payment
`
	found := documents([]byte(file), deployment)
	if len(found) != 1 {
		t.Fatalf("found %d documents", len(found))
	}
	if found[0].document != 2 {
		t.Fatalf("document = %d, want 2", found[0].document)
	}
}

func TestALeadingSeparatorDoesNotShiftTheCount(t *testing.T) {
	// Plenty of generators open a file with `---`. The first object in it is
	// still the first object.
	file := `---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-api
  namespace: payment
`
	found := documents([]byte(file), deployment)
	if len(found) != 1 || found[0].document != 0 {
		t.Fatalf("found %+v", found)
	}
}

func TestAnObjectOfAnotherKindOrGroupIsNotThisOne(t *testing.T) {
	for name, file := range map[string]string{
		"another kind": `apiVersion: apps/v1
kind: StatefulSet
metadata: {name: payment-api, namespace: payment}`,
		"another group": `apiVersion: extensions/v1beta1
kind: Deployment
metadata: {name: payment-api, namespace: payment}`,
		"another name": `apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-worker, namespace: payment}`,
		"another namespace": `apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api, namespace: payment-staging}`,
	} {
		t.Run(name, func(t *testing.T) {
			if found := documents([]byte(file), deployment); len(found) != 0 {
				t.Fatalf("matched: %+v", found)
			}
		})
	}
}

func TestAManifestWithNoNamespaceStillMatches(t *testing.T) {
	// A manifest that names no namespace is placed in whichever one the
	// Application sends it to. Insisting on a namespace here would miss most
	// of the manifests people actually write.
	file := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: payment-api
`
	if found := documents([]byte(file), deployment); len(found) != 1 {
		t.Fatalf("found %d documents", len(found))
	}
}

func TestACoreObjectIsMatchedOnTheEmptyGroup(t *testing.T) {
	// `apiVersion: v1` is the empty group and not a group called "v1". Getting
	// this wrong would make every ConfigMap and Service unfindable.
	configMap := identity{kind: "ConfigMap", namespace: "payment", name: "payment-config"}
	file := `apiVersion: v1
kind: ConfigMap
metadata:
  name: payment-config
  namespace: payment
`
	if found := documents([]byte(file), configMap); len(found) != 1 {
		t.Fatalf("found %d documents", len(found))
	}
}

func TestAnObjectInsideAListWrapperIsFound(t *testing.T) {
	// `kind: List` is how kubectl and several generators write more than one
	// object into a single document, and a repository kept that way would
	// otherwise read as empty.
	file := `apiVersion: v1
kind: List
items:
  - apiVersion: v1
    kind: Service
    metadata: {name: payment-api, namespace: payment}
  - apiVersion: apps/v1
    kind: Deployment
    metadata: {name: payment-api, namespace: payment}
`
	if found := documents([]byte(file), deployment); len(found) != 1 {
		t.Fatalf("found %d documents", len(found))
	}
}

func TestTwoFilesDeclaringTheSameNameAreBothReported(t *testing.T) {
	// A base beside its overlay is a real state of a repository. Both are
	// returned so the caller can say it is ambiguous rather than pick one.
	file := `apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api, namespace: payment}
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api}
`
	found := documents([]byte(file), deployment)
	if len(found) != 2 {
		t.Fatalf("found %d documents", len(found))
	}
}

func TestAFileThatIsNotAManifestIsSurvived(t *testing.T) {
	for name, file := range map[string]string{
		"empty":        "",
		"comments":     "# nothing here\n",
		"broken":       "kind: Deployment\n  bad indent: [\n",
		"a plain list": "- one\n- two\n",
		"a string":     "just a line of text\n",
		"separators":   "---\n---\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			if found := documents([]byte(file), deployment); len(found) != 0 {
				t.Fatalf("matched: %+v", found)
			}
		})
	}
}

func TestADocumentAfterABrokenOneIsNotClaimedFalsely(t *testing.T) {
	// The reader stops where it cannot parse. What matters is that it does not
	// report a match it never read.
	file := `kind: Deployment
  bad: [
---
apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api, namespace: payment}
`
	for _, found := range documents([]byte(file), deployment) {
		if !strings.Contains(found.content, "payment-api") {
			t.Fatalf("claimed a document it did not read: %+v", found)
		}
	}
}

func TestManifestFilesAreTheOnesWorthOpening(t *testing.T) {
	for name, want := range map[string]bool{
		"apps/deployment.yaml": true,
		"apps/service.yml":     true,
		"apps/patch.json":      true,
		"apps/DEPLOY.YAML":     true,
		"README.md":            false,
		"Chart.lock":           false,
		"scripts/deploy.sh":    false,
		"apps/values.yaml.tpl": false,
	} {
		if got := manifestFile(name); got != want {
			t.Fatalf("%s = %v, want %v", name, got, want)
		}
	}
}
