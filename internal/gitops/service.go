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
	"errors"
	"fmt"
	"path"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

// Compare reads the manifest that declares one object and holds it against the
// object itself.
//
// Locating and comparing are one call rather than two on purpose. A branch
// moves, and asking for the manifest and then asking for the difference would
// be two reads of it: the panel could show a file from one commit beside a
// difference computed against another, and nothing on screen would say so.
// Done together, both answers come from the commit resolved once at the top.
//
// It is deliberately not called when a drawer opens. Ownership costs a list
// and can be asked for freely; this costs a clone the first time a repository
// is seen, so it happens when somebody asks for it and not before.
func (s *Service) Compare(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.SourceState, error) {
	out := domain.SourceState{
		Ref:    ref,
		Search: domain.ManifestSearch{Ref: ref, Certainty: domain.ManifestUnknown},
	}

	ownership, err := s.argo.Ownership(ctx, clusterID, ref)
	if err != nil {
		return out, err
	}
	if !ownership.Confidence.Managed() {
		out.Search.Reason = ownership.Reason
		out.Comparison = unavailable(domain.BlockerUnmanaged,
			"No Application declares this object, so there is no source state to compare it with.")
		return out, nil
	}

	trees := treeSources(ownership.Sources)
	if len(trees) == 0 {
		// Helm, Kustomize, a plugin, a chart from a Helm repository: the
		// certainty already on the source says why there is no file better
		// than a search that came back empty would.
		out.Search.Certainty, out.Search.Reason = nothingToSearch(ownership.Sources)
		out.Comparison = unavailable(domain.BlockerGenerated, out.Search.Reason)
		return out, nil
	}

	live, err := s.read(ctx, clusterID, ref)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// A deleted object is not an object that matches its manifest, and
			// treating the two the same would be the most misleading thing
			// this panel could do.
			out.Search.Reason = "This object no longer exists in the cluster."
			out.Comparison = unavailable(domain.BlockerGone,
				"This object no longer exists in the cluster, so there is nothing to compare the manifest with.")
			return out, nil
		}
		return out, err
	}

	// The tree was known before the search began and stays known whatever it
	// turns up. Failing to find a file does not unlearn the directory.
	out.Search.Certainty = domain.ManifestTree

	var found []domain.ManifestLocation
	var refused refusal
	for _, source := range trees {
		result, err := s.search(ctx, source, live.identity)
		if err != nil {
			// A repository that cannot be read is an answer rather than an
			// error: the drawer keeps everything ownership established and
			// gains a sentence saying what to go and fix.
			if refused.message == "" {
				refused = declining(err)
			}
			continue
		}
		if out.Search.Commit == "" {
			out.Search.Commit = result.commit
		}
		out.Search.Scanned += result.scanned
		out.Search.Truncated = out.Search.Truncated || result.truncated
		found = append(found, result.locations...)
	}

	out.Search = describeSearch(out.Search, found, refused)
	out.Comparison = compareAgainst(out.Search, live, refused)
	if live != nil && out.Comparison.State != domain.ComparisonUnavailable &&
		hasReplicaDifference(out.Comparison.Differences) {
		out.Comparison = enrich(out.Comparison, s.gather(ctx, clusterID, ownership, live))
	}
	return out, nil
}

// refusal is git declining, kept as both the sentence and the whole of what
// git said. The sentence goes in the panel; the output goes behind a
// disclosure for the reader the sentence does not satisfy.
type refusal struct {
	message string
	output  string
}

// declining takes an error from a search and keeps git's own words with it.
//
// Only internal/git carries output worth showing. Anything else that fails
// here has a message that is already the whole of what is known.
func declining(err error) refusal {
	out := refusal{message: err.Error()}
	var failure *git.Error
	if errors.As(err, &failure) {
		out.output = failure.Output
	}
	return out
}

// describeSearch settles what the search found into a sentence.
func describeSearch(out domain.ManifestSearch, found []domain.ManifestLocation, refused refusal) domain.ManifestSearch {
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
	case refused.message != "":
		out.Reason = refused.message
		out.Output = refused.output
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
	return out
}

// compareAgainst holds the located manifest against the live object.
//
// Every way this can decline is a different situation with a different thing
// to do about it, so each gets its own blocker rather than one "comparison
// failed". None of them is an error: a Helm Application will never have a file
// to compare, and colouring that red would teach people to ignore the panel.
func compareAgainst(search domain.ManifestSearch, live *subject, refused refusal) domain.StateComparison {
	switch {
	case search.Certainty == domain.ManifestGenerated || search.Certainty == domain.ManifestUnknown:
		// Compare returns before reaching here for these, to avoid the clone.
		// The case is kept so the decision does not depend on which caller
		// asked: a certainty that names no file cannot be compared, wherever
		// that is noticed.
		return unavailable(domain.BlockerGenerated, search.Reason)
	case len(search.Candidates) > 1:
		return unavailable(domain.BlockerAmbiguous,
			"More than one document declares this object, so there is no single source state to compare against.")
	case refused.message != "" && search.Located == nil:
		return unavailable(domain.BlockerRepository, refused.message)
	case search.Located == nil:
		return unavailable(domain.BlockerNotLocated, search.Reason)
	}

	source, err := canonicalManifest([]byte(search.Located.Content))
	if err != nil {
		return unavailable(domain.BlockerManifestInvalid,
			fmt.Sprintf("%s could not be read as a Kubernetes object: %v", search.Located.Path, err))
	}
	current, err := canonicalObject(live.object.Object)
	if err != nil {
		return unavailable(domain.BlockerManifestInvalid, err.Error())
	}

	// Two passes with two different jobs. Normalisation erases differences
	// that were never differences, so a defaulted field never becomes a row at
	// all; classification explains the rows that are left. Hiding a
	// semantically equal field in a disclosure instead of normalising it away
	// would still be showing the reader something untrue, only more quietly.
	normalise(current, source)
	kind, _ := current["kind"].(string)
	differences := markImplicit(classify(compare(source, current, live.sensitive)), source, kind)
	out := tally(differences)

	switch {
	case out.Meaningful > 0:
		out.State = domain.ComparisonDiffers
		out.Reason = differReason(out)
	case out.SystemManaged > 0:
		// Not "identical". An object carrying its controller's revision
		// annotation does not match its manifest and never will, and a panel
		// that claims it does spends a reader's trust on a detail.
		out.State = domain.ComparisonEqual
		out.Reason = fmt.Sprintf(
			"Nothing differs except %d %s Kubernetes or Argo CD writes onto the object itself.",
			out.SystemManaged, plural(out.SystemManaged, "field", "fields"))
	default:
		out.State = domain.ComparisonEqual
		out.Reason = "Every field this comparison reads matches the source manifest."
	}
	return out
}

// tally sorts meaningful differences to the front and counts both classes.
func tally(differences []domain.StateDifference) domain.StateComparison {
	out := domain.StateComparison{}
	ordered := make([]domain.StateDifference, 0, len(differences))

	for _, difference := range differences {
		if difference.Class == domain.DifferenceMeaningful {
			out.Meaningful++
			if explained(difference) {
				out.Explained++
			}
			if difference.Explanation != nil && difference.Explanation.Attention == domain.AttentionReview {
				out.NeedsAttention++
			}
			ordered = append(ordered, difference)
		}
		out.Redacted = out.Redacted || difference.Redacted
	}
	for _, difference := range differences {
		if difference.Class != domain.DifferenceMeaningful {
			out.SystemManaged++
			ordered = append(ordered, difference)
		}
	}

	out.Differences = ordered
	return out
}

func hasReplicaDifference(differences []domain.StateDifference) bool {
	for _, difference := range differences {
		if difference.Path == "spec.replicas" {
			return true
		}
	}
	return false
}

func unavailable(blocker domain.ComparisonBlocker, reason string) domain.StateComparison {
	return domain.StateComparison{State: domain.ComparisonUnavailable, Blocker: blocker, Reason: reason}
}

func plural(count int, one, many string) string {
	if count == 1 {
		return one
	}
	return many
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

// subject is the live object a comparison is about.
//
// The object is carried alongside its identity because both come from one
// read. The search needs the identity to match documents and the comparison
// needs the object itself, and fetching it twice for one button press would be
// a second round trip for something already in hand.
type subject struct {
	object    *unstructured.Unstructured
	identity  identity
	sensitive bool
}

// read fetches the object and works out the name a manifest calls it by.
//
// The catalogue knows a kind as its plural resource name — "deployments" —
// because that is what the API server is addressed with. A manifest says
// `kind: Deployment`, and nothing in the catalogue carries that spelling, so
// the object itself is asked.
//
// This is one GET of one object. The ownership call above reads the same
// object for its own purposes, which is one more round trip than strictly
// necessary and nothing at all beside the clone that follows.
func (s *Service) read(ctx context.Context, clusterID string, ref domain.ResourceRef) (*subject, error) {
	info, ok := s.clusters.LookupKind(clusterID, ref.Kind)
	if !ok {
		return nil, fmt.Errorf("unknown resource type %q", ref.Kind)
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, err
	}

	resource := client.Dynamic.Resource(kube.GVRFor(info.Group, info.Version, info.Resource))
	var object *unstructured.Unstructured
	if info.Namespaced {
		object, err = resource.Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	} else {
		object, err = resource.Get(ctx, ref.Name, metav1.GetOptions{})
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", ref.Name, err)
	}

	group := ""
	if before, _, found := strings.Cut(object.GetAPIVersion(), "/"); found {
		group = before
	}
	return &subject{
		object: object,
		// Sensitive is the catalogue's existing word for a kind whose values
		// stay hidden until asked for, and it is what keeps the rule about
		// Secrets in one place rather than spelled out again here.
		sensitive: info.Sensitive,
		identity: identity{
			group:     group,
			kind:      object.GetKind(),
			namespace: object.GetNamespace(),
			name:      object.GetName(),
		},
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
