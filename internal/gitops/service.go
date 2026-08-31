// Package gitops connects a live Kubernetes object to the file that declares
// it.
//
// It sits above two packages that do not know about each other: internal/argocd
// answers which Application manages an object and where that Application says
// its manifests live, and internal/git reads a repository without holding a
// credential. Neither has any business knowing what the other is for, so the
// question that needs both of them is asked here.
//
// The answer is allowed to be "I could not find it". A repository is searched
// rather than indexed, the search is bounded, and a directory of Helm
// templates contains no file that equals a running object at all. Every one of
// those outcomes is reported as itself instead of as a failure, because the
// certainty this package produces is what a later slice will edit files on the
// strength of.
package gitops

import (
	"context"
	"fmt"
	"path"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/argocd"
	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/git"
	"biebie-kube/internal/kube"
)

// fileBudget bounds how many files one search reads.
//
// A directory of manifests for one application holds a handful. A repository
// that keeps every environment in one folder holds hundreds, and reading all
// of them to answer one drawer is not a trade worth making — stopping early
// and saying so is.
const fileBudget = 400

// fileSizeLimit skips a file too large to be a manifest somebody wrote.
//
// A repository that vendors every CRD in the world has one of these, and
// pulling it through a pipe costs more than the answer is worth.
const fileSizeLimit = 1 << 20

// Service finds the manifest behind a live object.
type Service struct {
	clusters *cluster.Manager
	argo     *argocd.Service
	cache    *git.Cache
}

// NewService wires the parts a search needs.
func NewService(clusters *cluster.Manager, argo *argocd.Service, cache *git.Cache) *Service {
	return &Service{clusters: clusters, argo: argo, cache: cache}
}

// Locate looks for the document that declares one object.
//
// It is deliberately not called when a drawer opens. Ownership costs a list
// and can be asked for freely; this costs a clone the first time a repository
// is seen, so it happens when somebody asks for it and not before.
func (s *Service) Locate(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.ManifestSearch, error) {
	out := domain.ManifestSearch{Ref: ref, Certainty: domain.ManifestUnknown}

	ownership, err := s.argo.Ownership(ctx, clusterID, ref)
	if err != nil {
		return out, err
	}
	if !ownership.Confidence.Managed() {
		out.Reason = ownership.Reason
		return out, nil
	}

	trees := treeSources(ownership.Sources)
	if len(trees) == 0 {
		// Helm, Kustomize, a plugin, a chart from a Helm repository: the
		// certainty already on the source says why there is no file better
		// than a search that came back empty would.
		out.Certainty, out.Reason = nothingToSearch(ownership.Sources)
		return out, nil
	}

	want, err := s.identify(ctx, clusterID, ref)
	if err != nil {
		return out, err
	}

	// The tree was known before the search began and stays known whatever it
	// turns up. Failing to find a file does not unlearn the directory.
	out.Certainty = domain.ManifestTree

	var found []domain.ManifestLocation
	var refusal string
	for _, source := range trees {
		result, err := s.search(ctx, source, want)
		if err != nil {
			// A repository that cannot be read is an answer rather than an
			// error: the drawer keeps everything ownership established and
			// gains a sentence saying what to go and fix.
			if refusal == "" {
				refusal = err.Error()
			}
			continue
		}
		if out.Commit == "" {
			out.Commit = result.commit
		}
		out.Scanned += result.scanned
		out.Truncated = out.Truncated || result.truncated
		found = append(found, result.locations...)
	}

	switch {
	case len(found) == 1:
		out.Certainty = domain.ManifestExact
		out.Located = &found[0]
		out.Reason = fmt.Sprintf("%s declares this object.", found[0].Path)
	case len(found) > 1:
		// Two files that both declare this name is a real state of a
		// repository — an overlay beside its base, two environments in one
		// tree — and picking one would be inventing an answer.
		out.Candidates = found
		out.Reason = fmt.Sprintf(
			"%d documents declare an object with this name. The repository does not say which of them is the one running.",
			len(found))
	case refusal != "":
		out.Reason = refusal
	case out.Truncated:
		out.Reason = fmt.Sprintf(
			"The first %d files in this directory do not declare this object, and there are more than this search reads.",
			out.Scanned)
	case out.Scanned == 0:
		out.Reason = "This path holds no YAML or JSON files at the revision the Application points at."
	default:
		out.Reason = fmt.Sprintf(
			"Nothing in the %d files under this path declares this object. It may be rendered from something this search does not read, or committed elsewhere.",
			out.Scanned)
	}
	return out, nil
}

// result is one repository's contribution to a search.
type result struct {
	commit    string
	locations []domain.ManifestLocation
	scanned   int
	truncated bool
}

// search reads one source's directory and matches what is in it.
func (s *Service) search(ctx context.Context, source domain.GitSource, want identity) (result, error) {
	// Every one of these three came off an object in the cluster, and none of
	// them reaches git without being checked first.
	remote, err := git.ParseRemote(source.RepoURL)
	if err != nil {
		return result{}, err
	}
	revision, err := git.ParseRevision(source.Revision)
	if err != nil {
		return result{}, err
	}
	directory, err := git.ParsePath(source.Path)
	if err != nil {
		return result{}, err
	}

	tree, err := s.cache.Open(ctx, remote, revision)
	if err != nil {
		return result{}, err
	}
	entries, err := tree.List(ctx, directory)
	if err != nil {
		return result{}, err
	}

	out := scan(ctx, tree, source, entries, want)
	out.commit = tree.Commit()
	return out, nil
}

// reader is the part of a git tree a scan needs.
//
// Narrowed to one method so the budget below can be tested without a
// repository: an off-by-one here turns "there are more files than this reads"
// into a confident "nothing declares this object", and that is a wrong answer
// rather than a missing one.
type reader interface {
	Read(ctx context.Context, file string) ([]byte, error)
}

// scan opens the files worth opening and matches what is in them.
func scan(ctx context.Context, tree reader, source domain.GitSource, entries []git.Entry, want identity) result {
	var out result
	for _, entry := range entries {
		if !manifestFile(entry.Path) || entry.Size > fileSizeLimit {
			continue
		}
		if out.scanned >= fileBudget {
			out.truncated = true
			break
		}
		out.scanned++

		content, err := tree.Read(ctx, entry.Path)
		if err != nil {
			// One unreadable file is not a reason to abandon the directory.
			continue
		}
		for _, found := range documents(content, want) {
			out.locations = append(out.locations, domain.ManifestLocation{
				Repository: source.Repository,
				Path:       entry.Path,
				Document:   found.document,
				Content:    found.content,
			})
		}
	}
	return out
}

// identify reads the object to learn the kind and group a manifest names it by.
//
// The catalogue knows a kind as its plural resource name — "deployments" —
// because that is what the API server is addressed with. A manifest says
// `kind: Deployment`, and nothing in the catalogue carries that spelling, so
// the object itself is asked. It is a second read of something the ownership
// call above already fetched, which is a fair price beside the clone this is
// about to do.
func (s *Service) identify(ctx context.Context, clusterID string, ref domain.ResourceRef) (identity, error) {
	info, ok := s.clusters.LookupKind(clusterID, ref.Kind)
	if !ok {
		return identity{}, fmt.Errorf("unknown resource type %q", ref.Kind)
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return identity{}, err
	}

	resource := client.Dynamic.Resource(kube.GVRFor(info.Group, info.Version, info.Resource))
	var object *unstructured.Unstructured
	if info.Namespaced {
		object, err = resource.Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	} else {
		object, err = resource.Get(ctx, ref.Name, metav1.GetOptions{})
	}
	if err != nil {
		return identity{}, fmt.Errorf("read %s: %w", ref.Name, err)
	}

	group := ""
	if before, _, found := strings.Cut(object.GetAPIVersion(), "/"); found {
		group = before
	}
	return identity{
		group:     group,
		kind:      object.GetKind(),
		namespace: object.GetNamespace(),
		name:      object.GetName(),
	}, nil
}

// treeSources picks the sources that are directories of manifests.
func treeSources(sources []domain.GitSource) []domain.GitSource {
	var out []domain.GitSource
	for _, source := range sources {
		if source.Certainty == domain.ManifestTree {
			out = append(out, source)
		}
	}
	return out
}

// nothingToSearch explains an Application whose manifests are not files.
func nothingToSearch(sources []domain.GitSource) (domain.ManifestCertainty, string) {
	for _, source := range sources {
		if source.Certainty == domain.ManifestGenerated {
			return domain.ManifestGenerated, source.Note
		}
	}
	return domain.ManifestUnknown, "This Application does not point at a directory of manifests."
}

// manifestFile reports a file worth opening.
func manifestFile(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".yaml", ".yml", ".json":
		return true
	default:
		return false
	}
}
