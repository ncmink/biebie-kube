package authoring

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// These tests are about the checks that run before anything is sent. Every one
// of them is a mistake the API server would also catch — later, less clearly,
// and in some cases only after the first two objects of three were created.

func TestValidYAMLBecomesOneParsedObject(t *testing.T) {
	objects, problems := parseAll(`
apiVersion: v1
kind: ConfigMap
metadata:
  name: example
  namespace: reporting
data: {}
`)

	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
	if len(objects) != 1 || objects[0].GetName() != "example" {
		t.Fatalf("objects = %+v", objects)
	}
}

func TestInvalidYAMLIsRejectedWithTheDocumentItIsIn(t *testing.T) {
	_, problems := parseAll(`
apiVersion: v1
kind: ConfigMap
---
: this is not
  valid: yaml
   at all
`)

	if len(problems) != 1 {
		t.Fatalf("problems = %+v", problems)
	}
	if problems[0].Resource != 1 {
		t.Fatalf("the problem was blamed on document %d", problems[0].Resource)
	}
}

func TestAMultiDocumentManifestKeepsEveryObject(t *testing.T) {
	// cdk8s synthesises one file that may declare a Deployment, a Service and
	// a ConfigMap. A parser that read the first document would create one of
	// the three and report success.
	objects, problems := parseAll(`apiVersion: v1
kind: ConfigMap
metadata:
  name: config
---
apiVersion: v1
kind: Service
metadata:
  name: service
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: deployment
`)

	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
	if len(objects) != 3 {
		t.Fatalf("parsed %d documents", len(objects))
	}
	if objects[2].GetKind() != "Deployment" {
		t.Fatalf("last object = %q", objects[2].GetKind())
	}
}

func TestALeadingSeparatorAndTrailingCommentsAreNotObjects(t *testing.T) {
	objects, problems := parseAll(`---
apiVersion: v1
kind: ConfigMap
metadata:
  name: config
---
# nothing here yet
`)

	if len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
	if len(objects) != 1 {
		t.Fatalf("parsed %d documents", len(objects))
	}
}

func TestAnEmptyManifestIsSaidToBeEmpty(t *testing.T) {
	_, problems := parseAll("   \n\n")

	if len(problems) != 1 || !strings.Contains(problems[0].Message, "empty") {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestTheThreeIdentityFieldsAreEachChecked(t *testing.T) {
	for name, test := range map[string]struct {
		object *unstructured.Unstructured
		says   string
	}{
		"no apiVersion": {document("", "ConfigMap", "reporting", "example"), "no apiVersion"},
		"no kind":       {document("v1", "", "reporting", "example"), "no kind"},
		"no name":       {document("v1", "ConfigMap", "reporting", ""), "no metadata.name"},
	} {
		problems := identityProblems(0, test.object)
		if len(problems) != 1 || !strings.Contains(problems[0].Message, test.says) {
			t.Fatalf("%s: problems = %+v", name, problems)
		}
	}
}

func TestGenerateNameIsRefusedRatherThanQuietlyAccepted(t *testing.T) {
	// A create whose name the manifest does not contain is a create whose
	// result cannot be previewed, and the preview is the feature.
	obj := document("v1", "ConfigMap", "reporting", "")
	_ = unstructured.SetNestedField(obj.Object, "example-", "metadata", "generateName")

	problems := identityProblems(0, obj)

	if len(problems) != 1 || !strings.Contains(problems[0].Message, "generateName") {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestANamespaceMismatchIsRefusedAndNotRewritten(t *testing.T) {
	// Rewriting is the failure worth preventing: an application that quietly
	// changed the namespace to match the sidebar would eventually create the
	// right object in the wrong place and report success.
	obj := document("v1", "ConfigMap", "production", "example")

	problem := namespaceProblem(0, obj, "reporting", true, true)

	if problem == nil {
		t.Fatal("a manifest for another namespace was accepted")
	}
	if !strings.Contains(problem.Message, "does not rewrite") {
		t.Fatalf("message = %q", problem.Message)
	}
	if obj.GetNamespace() != "production" {
		t.Fatalf("the manifest was modified: namespace is now %q", obj.GetNamespace())
	}
}

func TestAMatchingNamespaceIsNoProblem(t *testing.T) {
	obj := document("v1", "ConfigMap", "reporting", "example")

	if problem := namespaceProblem(0, obj, "reporting", true, true); problem != nil {
		t.Fatalf("problem = %+v", problem)
	}
}

func TestANamespacedObjectWithoutANamespaceIsAskedForOne(t *testing.T) {
	obj := document("v1", "ConfigMap", "", "example")

	problem := namespaceProblem(0, obj, "reporting", true, true)

	if problem == nil || !strings.Contains(problem.Message, "reporting") {
		t.Fatalf("problem = %+v", problem)
	}
}

func TestAClusterScopedObjectIsHandledSeparately(t *testing.T) {
	// A namespace on a ClusterRole is a mistake the API server reports less
	// clearly than this does, and an absent namespace on one is correct.
	withNamespace := document("rbac.authorization.k8s.io/v1", "ClusterRole", "reporting", "reader")
	if problem := namespaceProblem(0, withNamespace, "reporting", false, true); problem == nil {
		t.Fatal("a namespace on a cluster-scoped object was accepted")
	}

	without := document("rbac.authorization.k8s.io/v1", "ClusterRole", "", "reader")
	if problem := namespaceProblem(0, without, "reporting", false, true); problem != nil {
		t.Fatalf("a correct cluster-scoped object was refused: %+v", problem)
	}
}

func TestAKindTheClusterDoesNotServeIsNotJudgedOnItsNamespace(t *testing.T) {
	// Reporting "this should be cluster-scoped" about a kind nobody could look
	// up would be guessing, and the unknown kind is the problem worth showing.
	obj := document("acme.io/v1", "Widget", "reporting", "example")

	if problem := namespaceProblem(0, obj, "reporting", false, false); problem != nil {
		t.Fatalf("problem = %+v", problem)
	}
}

func TestTheSameObjectTwiceIsCaughtBeforeTheClusterSeesIt(t *testing.T) {
	// Kubernetes would accept the first and refuse the second with
	// AlreadyExists, which reads as "somebody else already made this" rather
	// than "your manifest contains it twice".
	objects, _ := parseAll(`apiVersion: v1
kind: ConfigMap
metadata:
  name: example
  namespace: reporting
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: example
  namespace: reporting
`)

	problems := duplicateProblems(objects)

	if len(problems) != 1 || problems[0].Resource != 1 {
		t.Fatalf("problems = %+v", problems)
	}
	if !strings.Contains(problems[0].Message, "document 1") {
		t.Fatalf("message does not name the other document: %q", problems[0].Message)
	}
}

func TestTwoObjectsOfTheSameNameInDifferentNamespacesAreNotDuplicates(t *testing.T) {
	objects, _ := parseAll(`apiVersion: v1
kind: ConfigMap
metadata:
  name: example
  namespace: reporting
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: example
  namespace: staging
`)

	if problems := duplicateProblems(objects); len(problems) != 0 {
		t.Fatalf("problems = %+v", problems)
	}
}

func TestRenderingKeepsEveryDocument(t *testing.T) {
	// The preview and the write have to be the same text. A renderer that lost
	// a document would show three objects and create two.
	objects, _ := parseAll(`apiVersion: v1
kind: ConfigMap
metadata:
  name: one
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: two
`)

	out, err := render(objects)
	if err != nil {
		t.Fatal(err)
	}

	again, problems := parseAll(out)
	if len(problems) != 0 || len(again) != 2 {
		t.Fatalf("round trip lost something: %d objects, %+v", len(again), problems)
	}
}

func document(apiVersion, kind, namespace, name string) *unstructured.Unstructured {
	metadata := map[string]any{}
	if namespace != "" {
		metadata["namespace"] = namespace
	}
	if name != "" {
		metadata["name"] = name
	}
	object := map[string]any{"metadata": metadata}
	if apiVersion != "" {
		object["apiVersion"] = apiVersion
	}
	if kind != "" {
		object["kind"] = kind
	}
	return &unstructured.Unstructured{Object: object}
}
