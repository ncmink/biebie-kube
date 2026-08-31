package argocd

import (
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

func TestATrackingAnnotationNamesTheApplication(t *testing.T) {
	deployment := object("apps/v1", "Deployment", "payment", "payment-api", nil, map[string]string{
		trackingAnnotation: "payment-api:apps/Deployment:payment/payment-api",
	})
	apps := []*unstructured.Unstructured{argoApp("argocd", "payment-api", gitSpec("apps/payment/prod"), nil)}

	out := resolveOwnership(deploymentRef(), deployment, apps)

	if out.Confidence != domain.OwnershipTracked {
		t.Fatalf("confidence = %q (%s)", out.Confidence, out.Reason)
	}
	if out.App == nil || out.App.Name != "payment-api" {
		t.Fatalf("app = %+v", out.App)
	}
}

func TestATrackingAnnotationAboutAnotherObjectIsIgnored(t *testing.T) {
	// An annotation copied by `kubectl apply -f` onto a different resource
	// names an Application that never created it. The identity inside the
	// value is the only thing that catches that, so it has to be checked.
	deployment := object("apps/v1", "Deployment", "payment", "payment-api", nil, map[string]string{
		trackingAnnotation: "payment-api:apps/Deployment:payment/something-else",
	})
	apps := []*unstructured.Unstructured{argoApp("argocd", "payment-api", gitSpec("apps/payment/prod"), nil)}

	out := resolveOwnership(deploymentRef(), deployment, apps)

	if out.Confidence != domain.OwnershipUnmanaged {
		t.Fatalf("a stale tracking annotation was believed: %q", out.Confidence)
	}
}

func TestAnInstanceLabelAloneIsOnlyACandidate(t *testing.T) {
	// `app.kubernetes.io/instance` is a recommended Kubernetes label that Helm
	// sets on everything it installs. A resource carrying it beside an
	// Application of the same name may have nothing to do with Argo CD.
	deployment := object("apps/v1", "Deployment", "payment", "payment-api",
		map[string]string{instanceLabel: "payment-api"}, nil)
	apps := []*unstructured.Unstructured{argoApp("argocd", "payment-api", gitSpec("apps/payment/prod"), nil)}

	out := resolveOwnership(deploymentRef(), deployment, apps)

	if out.Confidence != domain.OwnershipCandidate {
		t.Fatalf("confidence = %q", out.Confidence)
	}
	if !strings.Contains(out.Reason, "Helm") {
		t.Fatalf("the reason does not warn why the label is weak: %q", out.Reason)
	}
}

func TestAnApplicationThatListsTheObjectConfirmsIt(t *testing.T) {
	deployment := object("apps/v1", "Deployment", "payment", "payment-api", nil, nil)
	apps := []*unstructured.Unstructured{
		argoApp("argocd", "payment-api", gitSpec("apps/payment/prod"), map[string]any{
			"resources": []any{
				map[string]any{"group": "apps", "kind": "Deployment", "namespace": "payment", "name": "payment-api"},
			},
		}),
	}

	out := resolveOwnership(deploymentRef(), deployment, apps)

	if out.Confidence != domain.OwnershipConfirmed {
		t.Fatalf("confidence = %q (%s)", out.Confidence, out.Reason)
	}
}

func TestACoreObjectIsMatchedOnTheEmptyGroup(t *testing.T) {
	// A core object's apiVersion is a bare "v1", which is the empty group and
	// not a group called "v1". Getting this wrong makes every ConfigMap and
	// Service in the cluster look unmanaged.
	configMap := object("v1", "ConfigMap", "payment", "payment-config", nil, map[string]string{
		trackingAnnotation: "payment-api:/ConfigMap:payment/payment-config",
	})
	apps := []*unstructured.Unstructured{
		argoApp("argocd", "payment-api", gitSpec("apps/payment/prod"), map[string]any{
			"resources": []any{
				map[string]any{"kind": "ConfigMap", "namespace": "payment", "name": "payment-config"},
			},
		}),
	}

	out := resolveOwnership(
		domain.ResourceRef{Kind: domain.KindConfigMap, Namespace: "payment", Name: "payment-config"},
		configMap, apps)

	if out.Confidence != domain.OwnershipTracked {
		t.Fatalf("confidence = %q (%s)", out.Confidence, out.Reason)
	}
}

func TestNothingClaimingTheObjectLeavesItUnmanaged(t *testing.T) {
	deployment := object("apps/v1", "Deployment", "payment", "payment-api", nil, nil)
	apps := []*unstructured.Unstructured{argoApp("argocd", "other-app", gitSpec("apps/other"), nil)}

	out := resolveOwnership(deploymentRef(), deployment, apps)

	if out.Confidence != domain.OwnershipUnmanaged {
		t.Fatalf("confidence = %q", out.Confidence)
	}
	if out.App != nil {
		t.Fatalf("an unmanaged object was given an application: %+v", out.App)
	}
}

func TestARepositoryURLNeverCarriesItsCredentials(t *testing.T) {
	// This is the security boundary: an Application may legitimately hold a
	// remote with a token in it, and that token must not reach the frontend,
	// a log line or an error message.
	for name, test := range map[string]struct{ raw, want string }{
		"https token":    {"https://oauth2:ghp_supersecret@github.com/acme/infra.git", "https://github.com/acme/infra.git"},
		"https user":     {"https://someone@github.com/acme/infra.git", "https://github.com/acme/infra.git"},
		"ssh password":   {"ssh://git:hunter2@git.acme.internal/acme/infra.git", "ssh://git@git.acme.internal/acme/infra.git"},
		"scp style kept": {"git@github.com:acme/infra.git", "git@github.com:acme/infra.git"},
		"plain https":    {"https://github.com/acme/infra.git", "https://github.com/acme/infra.git"},
	} {
		t.Run(name, func(t *testing.T) {
			got := sanitiseRepoURL(test.raw)
			if got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
			for _, secret := range []string{"ghp_supersecret", "hunter2"} {
				if strings.Contains(got, secret) {
					t.Fatalf("a credential survived sanitising: %q", got)
				}
			}
		})
	}
}

func TestACredentialIsStrippedBeforeASourceIsDescribed(t *testing.T) {
	// The stripping has to happen where the source is read, not only where a
	// URL is displayed: everything downstream of here crosses the binding.
	source := describeSource(map[string]any{
		"repoURL":        "https://oauth2:ghp_supersecret@github.com/acme/infra.git",
		"targetRevision": "main",
		"path":           "apps/payment/prod",
	}, false)

	if strings.Contains(source.RepoURL, "ghp_supersecret") {
		t.Fatalf("repoUrl leaks a token: %q", source.RepoURL)
	}
	if source.Repository != "acme/infra" {
		t.Fatalf("repository = %q", source.Repository)
	}
}

func TestRepositoryNamesSurviveBothRemoteSpellings(t *testing.T) {
	for name, test := range map[string]struct{ raw, host, repository string }{
		"scp":          {"git@github.com:acme/infra.git", "github.com", "acme/infra"},
		"https":        {"https://github.com/acme/infra.git", "github.com", "acme/infra"},
		"no suffix":    {"https://github.com/acme/infra", "github.com", "acme/infra"},
		"self hosted":  {"https://git.acme.internal:8443/acme/infra.git", "git.acme.internal", "acme/infra"},
		"nested group": {"https://gitlab.com/platform/infra/cluster-config.git", "gitlab.com", "platform/infra/cluster-config"},
	} {
		t.Run(name, func(t *testing.T) {
			host, repository := splitRepoURL(test.raw)
			if host != test.host || repository != test.repository {
				t.Fatalf("got %q %q, want %q %q", host, repository, test.host, test.repository)
			}
		})
	}
}

func TestARenderedSourceIsNotPresentedAsAFile(t *testing.T) {
	// Helm, Kustomize and plugins all have a path, and reporting that path as
	// plain manifests would promise a file that does not exist.
	for name, source := range map[string]map[string]any{
		"helm":      {"repoURL": "https://github.com/acme/infra.git", "path": "charts/payment", "helm": map[string]any{}},
		"kustomize": {"repoURL": "https://github.com/acme/infra.git", "path": "overlays/prod", "kustomize": map[string]any{}},
		"plugin":    {"repoURL": "https://github.com/acme/infra.git", "path": "apps/payment", "plugin": map[string]any{}},
		"chart":     {"repoURL": "https://charts.acme.io", "chart": "payment"},
	} {
		t.Run(name, func(t *testing.T) {
			got := describeSource(source, false)
			if got.Certainty != domain.ManifestGenerated {
				t.Fatalf("certainty = %q", got.Certainty)
			}
			if got.Note == "" {
				t.Fatal("a generated source must say why no file can be named")
			}
		})
	}
}

func TestAPlainDirectoryKnowsItsTreeAndSaysTheFileIsNot(t *testing.T) {
	got := describeSource(map[string]any{
		"repoURL": "https://github.com/acme/infra.git",
		"path":    "apps/payment/prod",
	}, false)

	if got.Certainty != domain.ManifestTree {
		t.Fatalf("certainty = %q", got.Certainty)
	}
	if got.Renderer != domain.RendererDirectory {
		t.Fatalf("renderer = %q", got.Renderer)
	}
	// An Application that leaves targetRevision out tracks HEAD, and saying
	// nothing would read as "no revision" rather than "the default one".
	if got.Revision != "HEAD" {
		t.Fatalf("revision = %q", got.Revision)
	}
}

func TestEverySourceOfAMultiSourceApplicationIsReported(t *testing.T) {
	app := argoApp("argocd", "payment-api", map[string]any{
		"sources": []any{
			map[string]any{"repoURL": "https://github.com/acme/infra.git", "path": "apps/payment/prod", "targetRevision": "main"},
			map[string]any{"repoURL": "https://github.com/acme/values.git", "ref": "values", "targetRevision": "main"},
		},
	}, nil)

	sources := sourcesOf(app)
	if len(sources) != 2 {
		t.Fatalf("sources = %d", len(sources))
	}
	if sources[0].Certainty != domain.ManifestTree {
		t.Fatalf("the manifest source reads as %q", sources[0].Certainty)
	}
	// A ref-only source supplies values to another source and produces no
	// manifests, so pointing an editor at it would open the wrong repository.
	if sources[1].Certainty != domain.ManifestUnknown {
		t.Fatalf("the values source reads as %q", sources[1].Certainty)
	}
	if !strings.Contains(sources[0].Note, "which of an Application's sources") {
		t.Fatalf("a multi-source note is missing: %q", sources[0].Note)
	}
}

func TestASingleSourceApplicationIsStillRead(t *testing.T) {
	app := argoApp("argocd", "payment-api", gitSpec("apps/payment/prod"), nil)

	sources := sourcesOf(app)
	if len(sources) != 1 || sources[0].Path != "apps/payment/prod" {
		t.Fatalf("sources = %+v", sources)
	}
	if strings.Contains(sources[0].Note, "which of an Application's sources") {
		t.Fatal("a single-source Application was given the multi-source caveat")
	}
}

func TestAnApplicationSetGeneratedApplicationSaysSo(t *testing.T) {
	app := argoApp("argocd", "payment-api", gitSpec("apps/payment/prod"), nil)
	app.SetOwnerReferences(ownerReference("ApplicationSet", "payment-per-env"))

	deployment := object("apps/v1", "Deployment", "payment", "payment-api", nil, map[string]string{
		trackingAnnotation: "payment-api:apps/Deployment:payment/payment-api",
	})

	out := resolveOwnership(deploymentRef(), deployment, []*unstructured.Unstructured{app})

	// Editing a generated Application is pointless: the generator rewrites it.
	if out.GeneratedBy != "payment-per-env" {
		t.Fatalf("generatedBy = %q", out.GeneratedBy)
	}
}

func TestAnApplicationOutsideTheControlPlaneNamespaceIsMatched(t *testing.T) {
	// "Applications in any namespace" names the instance `namespace_name`, and
	// an installation using it would otherwise look like it manages nothing.
	deployment := object("apps/v1", "Deployment", "payment", "payment-api", nil, map[string]string{
		trackingAnnotation: "team-payment_payment-api:apps/Deployment:payment/payment-api",
	})
	apps := []*unstructured.Unstructured{argoApp("team-payment", "payment-api", gitSpec("apps/payment/prod"), nil)}

	out := resolveOwnership(deploymentRef(), deployment, apps)

	if out.Confidence != domain.OwnershipTracked {
		t.Fatalf("confidence = %q (%s)", out.Confidence, out.Reason)
	}
}

func ownerReference(kind, name string) []metav1.OwnerReference {
	return []metav1.OwnerReference{{APIVersion: "argoproj.io/v1alpha1", Kind: kind, Name: name}}
}

func deploymentRef() domain.ResourceRef {
	return domain.ResourceRef{Kind: domain.KindDeployment, Namespace: "payment", Name: "payment-api"}
}

func gitSpec(path string) map[string]any {
	return map[string]any{
		"source": map[string]any{
			"repoURL":        "git@github.com:acme/platform-infra.git",
			"targetRevision": "main",
			"path":           path,
		},
	}
}

func object(apiVersion, kind, namespace, name string, labels, annotations map[string]string) *unstructured.Unstructured {
	metadata := map[string]any{"name": name, "namespace": namespace}
	if len(labels) > 0 {
		metadata["labels"] = toAny(labels)
	}
	if len(annotations) > 0 {
		metadata["annotations"] = toAny(annotations)
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion,
		"kind":       kind,
		"metadata":   metadata,
	}}
}

func argoApp(namespace, name string, spec, status map[string]any) *unstructured.Unstructured {
	object := map[string]any{
		"apiVersion": "argoproj.io/v1alpha1",
		"kind":       "Application",
		"metadata":   map[string]any{"name": name, "namespace": namespace},
		"spec":       spec,
	}
	if status != nil {
		object["status"] = status
	}
	return &unstructured.Unstructured{Object: object}
}

func toAny(values map[string]string) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
