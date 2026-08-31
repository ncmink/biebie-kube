package argocd

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

const (
	// TrackingAnnotation is what Argo CD writes when its resource tracking
	// method is `annotation`. The value names the Application and repeats the
	// object's own identity, which is what makes it checkable rather than
	// merely suggestive.
	//
	// It is exported because it is also the one annotation a comparison
	// against Git must ignore: Argo CD writes it onto the object after
	// applying, so it is never in the manifest and would show as a difference
	// on every managed object in the cluster.
	TrackingAnnotation = "argocd.argoproj.io/tracking-id"

	// InstanceLabel is Argo CD's default tracking method, and also the label
	// the Kubernetes recommended-labels convention tells every tool to set.
	// Helm sets it on everything it installs. Finding it therefore raises a
	// candidate and never a conclusion.
	//
	// It is exported for the same reason as the annotation above: a comparison
	// against Git meets it on the live object and never in the repository.
	InstanceLabel = "app.kubernetes.io/instance"
)

// Ownership answers where one live object's desired state lives.
//
// This is the bridge the product is built on: an object a repository declares
// is edited in that repository, and an object nothing declares is operated on
// directly. Answering it wrongly sends an engineer to edit a file that has
// nothing to do with what is running, so every answer carries how it was
// reached.
func (s *Service) Ownership(ctx context.Context, clusterID string, ref domain.ResourceRef) (domain.ResourceOwnership, error) {
	out := domain.ResourceOwnership{Ref: ref, Confidence: domain.OwnershipUnmanaged}

	// The catalogue is read rather than the API, so a cluster without Argo CD
	// costs this call nothing at all. That is what lets the drawer ask on
	// every open instead of hiding the answer behind a button.
	if !s.Installed(clusterID) {
		out.Reason = "Argo CD is not installed in this cluster, so nothing here is managed from Git."
		return out, nil
	}

	info, ok := s.clusters.LookupKind(clusterID, ref.Kind)
	if !ok {
		return out, fmt.Errorf("unknown resource type %q", ref.Kind)
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return out, err
	}

	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)
	var obj *unstructured.Unstructured
	if info.Namespaced {
		obj, err = client.Dynamic.Resource(gvr).Namespace(ref.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
	} else {
		obj, err = client.Dynamic.Resource(gvr).Get(ctx, ref.Name, metav1.GetOptions{})
	}
	if err != nil {
		return out, fmt.Errorf("read %s: %w", ref.Name, err)
	}

	apps, err := listAll(ctx, client, applicationGVR)
	if err != nil {
		// An account that may read the object but not the Applications cannot
		// be told anything about Git. Saying that is better than failing the
		// drawer over a panel that is not the reason it was opened.
		out.Installed = true
		out.Reason = "This account cannot list Argo CD Applications, so Git ownership could not be checked."
		return out, nil
	}

	resolved := resolveOwnership(ref, obj, apps)
	resolved.Installed = true
	return resolved, nil
}

// How firmly an Application can be tied to an object, weakest first. These
// exist so the loop can compare claims; they become a domain confidence once
// the best one is known.
const (
	rankNone = iota
	rankCandidate
	rankConfirmed
	rankTracked
)

// resolveOwnership decides which Application manages one live object.
//
// It takes the objects rather than a cluster so the decision — which is the
// part that can be wrong in a way nobody notices — is testable against
// hand-written Applications.
func resolveOwnership(
	ref domain.ResourceRef,
	obj *unstructured.Unstructured,
	apps []*unstructured.Unstructured,
) domain.ResourceOwnership {
	out := domain.ResourceOwnership{
		Ref:        ref,
		Confidence: domain.OwnershipUnmanaged,
		Reason:     "No Argo CD Application claims this object. Its desired state is not in a repository this application can see, so operating on it directly is the correct thing to do.",
	}

	id := identify(obj)

	// The annotation counts only when it is about this object. One copied by a
	// careless `kubectl apply -f` onto a different resource names an
	// Application that never created it, and the identity inside the value is
	// how that is caught.
	tracked := ""
	if name, claimed, ok := parseTrackingID(obj.GetAnnotations()[TrackingAnnotation]); ok && claimed == id {
		tracked = name
	}
	labelled := obj.GetLabels()[InstanceLabel]

	// Applications are ordered so that two with equal claims resolve the same
	// way on every call. A drawer that names a different Application each time
	// it is opened is worse than one that names none.
	ordered := make([]*unstructured.Unstructured, len(apps))
	copy(ordered, apps)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].GetNamespace() != ordered[j].GetNamespace() {
			return ordered[i].GetNamespace() < ordered[j].GetNamespace()
		}
		return ordered[i].GetName() < ordered[j].GetName()
	})

	var best *unstructured.Unstructured
	bestRank := rankNone
	for _, app := range ordered {
		names := appNames(app)
		rank := rankNone
		switch {
		case tracked != "" && nameMatches(names, tracked):
			rank = rankTracked
		case listsResource(app, id):
			rank = rankConfirmed
		case labelled != "" && nameMatches(names, labelled):
			rank = rankCandidate
		}
		if rank > bestRank {
			best, bestRank = app, rank
		}
	}
	if best == nil {
		return out
	}

	app := describeApp(best)
	app.Reason, _ = attention(app, text(best, "status", "health", "message"))
	out.App = &app
	out.AppKind = domain.KindArgoApplication
	out.Sources = sourcesOf(best)
	out.GeneratedBy = generatorOf(best)

	switch bestRank {
	case rankTracked:
		out.Confidence = domain.OwnershipTracked
		out.Reason = "Argo CD's tracking annotation on this object names this Application."
	case rankConfirmed:
		out.Confidence = domain.OwnershipConfirmed
		out.Reason = "This Application lists the object among the resources it manages."
	case rankCandidate:
		out.Confidence = domain.OwnershipCandidate
		out.Reason = "The app.kubernetes.io/instance label names this Application, but the Application does not list this object among its resources. Helm sets that label too, so this may not be Argo CD's doing."
	}
	return out
}

// resourceID is an object's identity in the terms Argo CD records it.
type resourceID struct {
	group     string
	kind      string
	namespace string
	name      string
}

func identify(obj *unstructured.Unstructured) resourceID {
	// A core object's apiVersion is a bare "v1" with no group in front of it,
	// which is the empty group rather than a group called "v1".
	group := ""
	if before, _, found := strings.Cut(obj.GetAPIVersion(), "/"); found {
		group = before
	}
	return resourceID{
		group:     group,
		kind:      obj.GetKind(),
		namespace: obj.GetNamespace(),
		name:      obj.GetName(),
	}
}

// parseTrackingID reads Argo CD's tracking annotation, whose value is
// `<app>:<group>/<Kind>:<namespace>/<name>`.
func parseTrackingID(value string) (string, resourceID, bool) {
	parts := strings.Split(strings.TrimSpace(value), ":")
	if len(parts) != 3 {
		return "", resourceID{}, false
	}
	group, kind, ok := strings.Cut(parts[1], "/")
	if !ok {
		return "", resourceID{}, false
	}
	namespace, name, ok := strings.Cut(parts[2], "/")
	if !ok {
		return "", resourceID{}, false
	}
	if parts[0] == "" || kind == "" || name == "" {
		return "", resourceID{}, false
	}
	return parts[0], resourceID{group: group, kind: kind, namespace: namespace, name: name}, true
}

// appNames lists what an Application may be called in a tracking value.
//
// Argo CD uses the bare name for an Application in the control-plane
// namespace, and `namespace_name` for one placed elsewhere by "applications in
// any namespace". Both are accepted rather than guessing which installation
// this cluster is running.
func appNames(app *unstructured.Unstructured) []string {
	name := app.GetName()
	if namespace := app.GetNamespace(); namespace != "" {
		return []string{name, namespace + "_" + name}
	}
	return []string{name}
}

func nameMatches(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

// listsResource reports whether an Application counts this object among the
// resources it manages.
//
// This is Argo CD's own account of what it owns rather than an inference from
// a label, which is why it outranks one.
func listsResource(app *unstructured.Unstructured, id resourceID) bool {
	entries, _, _ := unstructured.NestedSlice(app.Object, "status", "resources")
	for _, raw := range entries {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if field(entry, "kind") == id.kind &&
			field(entry, "name") == id.name &&
			field(entry, "group") == id.group &&
			field(entry, "namespace") == id.namespace {
			return true
		}
	}
	return false
}

// generatorOf names the ApplicationSet that produced an Application.
//
// It matters because the desired state of a generated Application is the
// generator's template rather than the Application object: editing the
// Application would be overwritten on the next reconcile.
func generatorOf(app *unstructured.Unstructured) string {
	for _, reference := range app.GetOwnerReferences() {
		if reference.Kind == "ApplicationSet" {
			return reference.Name
		}
	}
	return ""
}

// sourcesOf reads where an Application says its desired state comes from.
//
// `spec.sources` is read before `spec.source` because an Application may
// declare either, and a multi-source Application that was read for its single
// source would be reported with the wrong repository rather than with none.
func sourcesOf(app *unstructured.Unstructured) []domain.GitSource {
	if entries, found, _ := unstructured.NestedSlice(app.Object, "spec", "sources"); found && len(entries) > 0 {
		out := make([]domain.GitSource, 0, len(entries))
		for _, raw := range entries {
			if source, ok := raw.(map[string]any); ok {
				out = append(out, describeSource(source, true))
			}
		}
		return out
	}
	if source, found, _ := unstructured.NestedMap(app.Object, "spec", "source"); found {
		return []domain.GitSource{describeSource(source, false)}
	}
	return nil
}

// describeSource works out how close to a file one source gets.
//
// The renderers are checked before the plain path, because a Kustomize or Helm
// source has a path as well and reporting it as plain manifests would promise
// a file that does not exist.
func describeSource(source map[string]any, multi bool) domain.GitSource {
	out := domain.GitSource{
		RepoURL:  sanitiseRepoURL(field(source, "repoURL")),
		Revision: field(source, "targetRevision"),
		Path:     field(source, "path"),
		Chart:    field(source, "chart"),
		Ref:      field(source, "ref"),
	}
	out.Host, out.Repository = splitRepoURL(out.RepoURL)
	if out.Revision == "" {
		// Argo CD's own default when the field is left out.
		out.Revision = "HEAD"
	}

	switch {
	case out.Chart != "":
		out.Renderer, out.Certainty = domain.RendererChart, domain.ManifestGenerated
		out.Note = "A chart from a Helm repository. There is no Git tree to browse; the values on this Application are the desired state."
	case has(source, "plugin"):
		out.Renderer, out.Certainty = domain.RendererPlugin, domain.ManifestGenerated
		out.Note = "A config-management plugin renders these manifests, so no file in the repository equals this object."
	case has(source, "kustomize"):
		out.Renderer, out.Certainty = domain.RendererKustomize, domain.ManifestGenerated
		out.Note = "Kustomize renders these manifests from bases and overlays, so this object may not appear whole in any one file."
	case has(source, "helm"):
		out.Renderer, out.Certainty = domain.RendererHelm, domain.ManifestGenerated
		out.Note = "Helm renders these manifests from templates and values, so no file in the repository equals this object."
	case out.Ref != "" && out.Path == "":
		out.Renderer, out.Certainty = domain.RendererDirectory, domain.ManifestUnknown
		out.Note = "This source only supplies values to another source; it produces no manifests of its own."
	case out.Path != "":
		out.Renderer, out.Certainty = domain.RendererDirectory, domain.ManifestTree
		out.Note = "Plain manifests. The directory is known; which file in it declares this object is not, until the repository is read."
	default:
		out.Renderer, out.Certainty = domain.RendererDirectory, domain.ManifestUnknown
		out.Note = "This Application does not say which directory its manifests come from."
	}

	if multi && out.Certainty != domain.ManifestUnknown {
		out.Note += " Argo CD does not record which of an Application's sources produced a given object."
	}
	return out
}

// sanitiseRepoURL removes credentials embedded in a repository URL.
//
// `https://user:ghp_secret@github.com/acme/infra.git` is a valid remote and an
// ordinary thing to find on an Application. The token in it must not reach the
// frontend, a log line or an error message, so the userinfo is dropped from
// every http(s) URL before the value travels anywhere.
//
// An scp-style remote keeps its account name: `git@github.com:acme/infra.git`
// carries an SSH user rather than a secret, and removing it would change where
// the URL points. A password is dropped whatever the scheme.
func sanitiseRepoURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || !strings.Contains(raw, "@") {
		return raw
	}

	parsed, err := url.Parse(raw)
	if err != nil || parsed.User == nil {
		// An scp-style remote is not a URL and does not parse as one. It
		// carries no password to remove, so it is returned as written.
		return raw
	}
	if scheme := strings.ToLower(parsed.Scheme); scheme == "http" || scheme == "https" {
		parsed.User = nil
	} else {
		parsed.User = url.User(parsed.User.Username())
	}
	return parsed.String()
}

// splitRepoURL derives the host and the owner-and-name pair a person reads.
//
// Both remote spellings are handled, and a nested GitLab group keeps its
// depth: "platform/infra/cluster-config" is the repository's name, not three.
func splitRepoURL(raw string) (string, string) {
	raw = strings.TrimSuffix(strings.TrimSpace(raw), ".git")
	if raw == "" {
		return "", ""
	}

	if !strings.Contains(raw, "://") {
		if _, rest, found := strings.Cut(raw, "@"); found {
			raw = rest
		}
		host, path, found := strings.Cut(raw, ":")
		if !found {
			return "", ""
		}
		return host, strings.Trim(path, "/")
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", ""
	}
	return parsed.Hostname(), strings.Trim(parsed.Path, "/")
}

func field(entry map[string]any, key string) string {
	value, _ := entry[key].(string)
	return value
}

// has reports that a source declares a renderer block, empty or not. Argo CD
// treats `kustomize: {}` as "this is a Kustomize source", and so does this.
func has(source map[string]any, key string) bool {
	value, found := source[key]
	return found && value != nil
}
