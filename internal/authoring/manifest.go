package authoring

import (
	"bufio"
	"errors"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"biebie-kube/internal/domain"
)

// split cuts a multi-document manifest into documents.
//
// A `---` on a line of its own, which is the whole of the YAML stream syntax
// that matters here. Doing it by hand rather than with a stream decoder keeps
// each document's own text available: a problem is reported against the
// document it is in, and a decoder that has already merged them cannot say
// which one that was.
//
// A `---` inside a block scalar would be cut wrongly by this. It is not a
// thing that appears in a Kubernetes manifest a person is authoring by hand,
// and the documents are parsed afterwards, so the mistake surfaces as a parse
// error on a document rather than as a silently wrong object.
func split(raw string) []string {
	var out []string
	var current []string

	scanner := bufio.NewScanner(strings.NewReader(raw))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimRight(line, " \t") == "---" {
			out = append(out, strings.Join(current, "\n"))
			current = nil
			continue
		}
		current = append(current, line)
	}
	out = append(out, strings.Join(current, "\n"))

	var kept []string
	for _, doc := range out {
		if strings.TrimSpace(stripComments(doc)) != "" {
			kept = append(kept, doc)
		}
	}
	return kept
}

// stripComments is only used to decide whether a document is empty. A file
// that is nothing but a comment is not an object, and reporting it as one with
// no kind would be a confusing way to say so.
func stripComments(doc string) string {
	var kept []string
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// parseAll turns a manifest into objects, or says which document refused.
//
// Every document is attempted rather than stopping at the first failure. A
// person who typed three objects and made two mistakes should see both.
func parseAll(raw string) ([]*unstructured.Unstructured, []domain.ManifestProblem) {
	docs := split(raw)
	if len(docs) == 0 {
		return nil, []domain.ManifestProblem{{Resource: -1, Message: "The manifest is empty."}}
	}

	objects := make([]*unstructured.Unstructured, len(docs))
	var problems []domain.ManifestProblem

	for i, doc := range docs {
		var content map[string]any
		if err := yaml.Unmarshal([]byte(doc), &content); err != nil {
			problems = append(problems, domain.ManifestProblem{
				Resource: i,
				Message:  "This is not valid YAML: " + oneLine(err.Error()),
			})
			continue
		}
		if content == nil {
			problems = append(problems, domain.ManifestProblem{
				Resource: i, Message: "This document declares no object.",
			})
			continue
		}
		objects[i] = &unstructured.Unstructured{Object: content}
	}
	return objects, problems
}

// identityProblems checks the three fields without which nothing can be sent.
//
// They are checked here rather than left to the API server because the API
// server's answer arrives after the write has been attempted, and this feature
// promises a preview that says what will happen before anything is sent.
func identityProblems(index int, obj *unstructured.Unstructured) []domain.ManifestProblem {
	var out []domain.ManifestProblem
	if strings.TrimSpace(obj.GetAPIVersion()) == "" {
		out = append(out, domain.ManifestProblem{Resource: index, Message: "This object has no apiVersion."})
	}
	if strings.TrimSpace(obj.GetKind()) == "" {
		out = append(out, domain.ManifestProblem{Resource: index, Message: "This object has no kind."})
	}
	if strings.TrimSpace(obj.GetName()) == "" {
		// generateName is a real Kubernetes feature and is refused anyway.
		// A create whose name the manifest does not contain is a create whose
		// result cannot be previewed, and the preview is the feature.
		if _, found, _ := unstructured.NestedString(obj.Object, "metadata", "generateName"); found {
			out = append(out, domain.ManifestProblem{
				Resource: index,
				Message:  "This object uses metadata.generateName. Biebie Kube creates objects by name, so the name has to be in the manifest.",
			})
		} else {
			out = append(out, domain.ManifestProblem{Resource: index, Message: "This object has no metadata.name."})
		}
	}
	return out
}

// namespaceProblem compares where the object says it goes with where the
// screen says it goes.
//
// Nothing is rewritten. A manifest that says `production` while the target is
// `reporting` is either a mistake or a manifest for somewhere else, and an
// application that quietly changed the namespace to match the sidebar would
// eventually create the right object in the wrong place and report success.
func namespaceProblem(index int, obj *unstructured.Unstructured, target string, namespaced, known bool) *domain.ManifestProblem {
	declared := strings.TrimSpace(obj.GetNamespace())

	if !known {
		return nil
	}

	if !namespaced {
		if declared != "" {
			return &domain.ManifestProblem{
				Resource: index,
				Message: fmt.Sprintf("%s is cluster-scoped, but this manifest puts it in namespace %q. Remove metadata.namespace.",
					obj.GetKind(), declared),
			}
		}
		return nil
	}

	if declared == "" {
		return &domain.ManifestProblem{
			Resource: index,
			Message: fmt.Sprintf("This object has no metadata.namespace. Add %q so the manifest says where it goes.",
				target),
		}
	}
	if target != "" && declared != target {
		return &domain.ManifestProblem{
			Resource: index,
			Message: fmt.Sprintf("This manifest is for namespace %q, but you are creating in %q. Biebie Kube does not rewrite the manifest; change one of them.",
				declared, target),
		}
	}
	return nil
}

// duplicateProblems catches two documents that declare the same object.
//
// Kubernetes would accept the first and refuse the second with AlreadyExists,
// which reads as "somebody else already made this" rather than "your manifest
// contains it twice".
func duplicateProblems(objects []*unstructured.Unstructured) []domain.ManifestProblem {
	seen := map[string]int{}
	var out []domain.ManifestProblem
	for i, obj := range objects {
		if obj == nil {
			continue
		}
		key := obj.GetAPIVersion() + "/" + obj.GetKind() + "/" + obj.GetNamespace() + "/" + obj.GetName()
		if first, found := seen[key]; found {
			out = append(out, domain.ManifestProblem{
				Resource: i,
				Message: fmt.Sprintf("This is the same object as document %d. A manifest cannot create one object twice.",
					first+1),
			})
			continue
		}
		seen[key] = i
	}
	return out
}

// render re-encodes objects so the preview and the write are the same text.
func render(objects []*unstructured.Unstructured) (string, error) {
	var parts []string
	for _, obj := range objects {
		if obj == nil {
			continue
		}
		encoded, err := yaml.Marshal(obj.Object)
		if err != nil {
			return "", fmt.Errorf("render YAML: %w", err)
		}
		parts = append(parts, strings.TrimSpace(string(encoded)))
	}
	if len(parts) == 0 {
		return "", errors.New("the manifest declares no object")
	}
	return strings.Join(parts, "\n---\n") + "\n", nil
}

func oneLine(text string) string {
	if index := strings.IndexByte(text, '\n'); index >= 0 {
		text = text[:index]
	}
	return strings.TrimSpace(text)
}
