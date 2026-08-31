package gitops

import (
	"bufio"
	"bytes"
	"strings"

	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/yaml"
)

// identity is what a live object and a manifest are compared by.
//
// Four fields and no more. A manifest in a repository has no UID, no
// resourceVersion and usually no status, so the only things both sides
// certainly carry are what the object is and what it is called.
type identity struct {
	group     string
	kind      string
	namespace string
	name      string
}

// match is one document that declares the object.
type match struct {
	document int
	content  string
}

// documents splits a file into YAML documents and returns the ones that
// declare the object.
//
// The index is kept along with the text because a great many repositories put
// a deployment, its service and its config map in one file separated by `---`.
// Naming the file without naming the part of it would be an answer that still
// leaves somebody reading.
func documents(file []byte, want identity) []match {
	var out []match

	reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(file)))
	index := 0
	for {
		document, err := reader.Read()
		if err != nil {
			// End of file, or a file that stopped parsing part of the way
			// through: either way the documents already read are still true.
			return out
		}
		if len(bytes.TrimSpace(document)) == 0 {
			// A file that opens or closes with a bare `---` has an empty chunk
			// there, and it is not a document anybody counts. The index is not
			// advanced for it, so the first object in the file is the first
			// one however the file is punctuated.
			continue
		}
		if declares(document, want) {
			out = append(out, match{document: index, content: string(document)})
		}
		index++
	}
}

// declares reports whether one document is this object.
func declares(document []byte, want identity) bool {
	var object map[string]any
	if err := yaml.Unmarshal(document, &object); err != nil || object == nil {
		return false
	}
	return names(object, want, 0)
}

// names walks a document, following a List wrapper into the objects inside it.
//
// `kind: List` is how `kubectl get -o yaml` and a fair number of generators
// write several objects into one document, and a repository that keeps its
// manifests that way would otherwise look empty. The depth is bounded because
// the file is untrusted input like everything else here.
func names(object map[string]any, want identity, depth int) bool {
	if items, ok := object["items"].([]any); ok && strings.HasSuffix(text(object, "kind"), "List") {
		if depth > 1 {
			return false
		}
		for _, raw := range items {
			if nested, ok := raw.(map[string]any); ok && names(nested, want, depth+1) {
				return true
			}
		}
		return false
	}

	if text(object, "kind") != want.kind {
		return false
	}
	// A core object's apiVersion is a bare "v1", which is the empty group
	// rather than a group called "v1".
	group := ""
	if before, _, found := strings.Cut(text(object, "apiVersion"), "/"); found {
		group = before
	}
	if group != want.group {
		return false
	}

	metadata, _ := object["metadata"].(map[string]any)
	if metadata == nil || text(metadata, "name") != want.name {
		return false
	}

	// A manifest that names no namespace is placed in whichever namespace the
	// Application sends it to, which is where this object is. Insisting on one
	// here would miss most of the manifests people actually write, and the
	// ambiguity it lets in is reported as ambiguity rather than guessed at.
	namespace := text(metadata, "namespace")
	return namespace == "" || namespace == want.namespace
}

func text(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return value
}
