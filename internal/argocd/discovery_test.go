package argocd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// These tests are about the difference between an answer and a silence.
//
// Every one of them protects the same product rule from the same direction: an
// ownership check that could not see the whole cluster has established nothing,
// and reporting nothing as "nothing claims this" is what turns a missing
// permission into a direct write against somebody's GitOps namespace.

func TestApplicationsAreSearchedAcrossTheWholeCluster(t *testing.T) {
	// The safety bug this exists to prevent: an Argo CD in `argocd` managing a
	// Deployment in `reporting`, with the navigator showing `reporting` only.
	// If the search were scoped to anything the UI chose, the owning
	// Application would be invisible and the Deployment would read as
	// unmanaged — with a direct write offered on the strength of a filter.
	client, actions := fakeCluster(
		claimApp("argocd", "super-report", "reporting", nil),
	)

	apps, truncated, err := listPaged(context.Background(), client, applicationGVR, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if truncated {
		t.Fatal("a single page of Applications was reported as cut short")
	}
	if len(apps) != 1 || apps[0].GetName() != "super-report" {
		t.Fatalf("apps = %v", names(apps))
	}

	for _, action := range actions() {
		if action.GetNamespace() != "" {
			t.Fatalf("the Application listing was scoped to namespace %q; ownership must not be namespaced",
				action.GetNamespace())
		}
	}
}

func TestALimitedNamespaceSearchSaysWhichNamespaceItAsked(t *testing.T) {
	// The fallback for a refused cluster-wide read. It is allowed to exist,
	// and it is not allowed to be mistaken for the whole cluster.
	client, actions := fakeCluster(
		claimApp("argocd", "super-report", "reporting", nil),
		claimApp("team-payment", "payment", "payment", nil),
	)

	apps, _, err := listPaged(context.Background(), client, applicationGVR, "team-payment")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(apps) != 1 || apps[0].GetName() != "payment" {
		t.Fatalf("apps = %v", names(apps))
	}
	if got := actions()[0].GetNamespace(); got != "team-payment" {
		t.Fatalf("namespace = %q", got)
	}
}

func TestAListingCutShortIsReportedAsCutShort(t *testing.T) {
	// A continue token means the cluster has more than was read. Treating the
	// prefix as the whole list is the quietest possible way to conclude that
	// nothing claims an object.
	client, _ := fakeCluster()
	client.Dynamic.(*dynamicfake.FakeDynamicClient).PrependReactor("list", "applications",
		func(k8stesting.Action) (bool, runtime.Object, error) {
			list := &unstructured.UnstructuredList{}
			list.SetContinue("there-is-more")
			return true, list, nil
		})

	_, truncated, err := listPaged(context.Background(), client, applicationGVR, "")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !truncated {
		t.Fatal("a listing that never ran out of continue tokens was reported as complete")
	}
}

func TestAForbiddenListingIsUnknownRatherThanAbsent(t *testing.T) {
	out := refused(search{installed: installYes}, "cluster", "",
		apierrors.NewForbidden(schema.GroupResource{Group: "argoproj.io", Resource: "applications"},
			"", errors.New("user cannot list resource")))

	if out.complete {
		t.Fatal("a refused search reported itself as complete, which lets an empty result read as an absence")
	}
	if out.uncertainty != domain.UncertaintyForbidden {
		t.Fatalf("uncertainty = %q", out.uncertainty)
	}
	if out.installed == installNo {
		t.Fatal("a permission failure was read as Argo CD not being installed")
	}

	probe := lastProbe(t, out)
	if probe.Result != domain.OwnershipProbeForbidden {
		t.Fatalf("probe result = %q", probe.Result)
	}
	if probe.Resource != "applications.argoproj.io" || probe.Verb != "list" || probe.Scope != "cluster" {
		t.Fatalf("the probe does not describe the permission that is missing: %+v", probe)
	}
}

func TestATimeoutIsUnknownAndSaysSo(t *testing.T) {
	out := refused(search{installed: installYes}, "cluster", "", context.DeadlineExceeded)

	if out.complete || out.uncertainty != domain.UncertaintyTimeout {
		t.Fatalf("complete = %v, uncertainty = %q", out.complete, out.uncertainty)
	}
	if lastProbe(t, out).Result != domain.OwnershipProbeTimeout {
		t.Fatal("a timeout was recorded as something else, so the screen would offer the wrong fix")
	}
}

func TestAnUnservedResourceEstablishesThatArgoIsAbsent(t *testing.T) {
	// The one path on which "nothing here is managed from Git" is a fact. The
	// API server answered, and what it said was that it does not serve the
	// resource at all.
	out := refused(search{}, "cluster", "",
		apierrors.NewNotFound(schema.GroupResource{Group: "argoproj.io", Resource: "applications"}, ""))

	if out.installed != installNo {
		t.Fatalf("installed = %v", out.installed)
	}
	if !out.complete {
		t.Fatal("a positively established absence was not reported as a complete search")
	}
	if out.uncertainty != domain.UncertaintyNone {
		t.Fatalf("uncertainty = %q; a settled answer must not carry one", out.uncertainty)
	}
}

func TestAnErrorDetailCarriesOneLineAndNoCredential(t *testing.T) {
	// What is shown is a Kubernetes forbidden message, which names the account,
	// the verb and the resource. Nothing here reads a request, so no header and
	// no token can reach it — and the first line is taken so a wrapped error
	// cannot drag a page of context onto a permission panel.
	detail := oneLine(errors.New("applications.argoproj.io is forbidden\nsecond line\nthird line"))

	if strings.Contains(detail, "\n") {
		t.Fatalf("detail spans lines: %q", detail)
	}
	if detail != "applications.argoproj.io is forbidden" {
		t.Fatalf("detail = %q", detail)
	}
}

func TestARefusalIsRememberedForLongerThanAnAnswer(t *testing.T) {
	// A denied permission is not going to start working within seconds. Asking
	// again per row is how a non-admin account ends up waiting on a check that
	// was never going to pass.
	answered := search{complete: true}
	denied := search{uncertainty: domain.UncertaintyForbidden}

	if ttlFor(denied) <= ttlFor(answered) {
		t.Fatalf("a refusal is cached for %s and an answer for %s", ttlFor(denied), ttlFor(answered))
	}

	at := time.Now()
	held := cache{now: func() time.Time { return at }}
	held.put("cluster", denied)

	at = at.Add(ttlFor(answered) + time.Second)
	if _, ok := held.get("cluster"); !ok {
		t.Fatal("the refusal was re-asked as soon as a successful answer would have expired")
	}

	at = at.Add(refusalTTL)
	if _, ok := held.get("cluster"); ok {
		t.Fatal("a refusal is remembered forever, so fixing the permission would never take effect")
	}
}

func TestASearchIsRunOnceAndReused(t *testing.T) {
	// Ownership is asked on every drawer open. A full Application listing per
	// row is what makes clicking down a list feel broken.
	service := &Service{}
	runs := 0
	build := func() search {
		runs++
		return search{complete: true}
	}

	service.cached("cluster", build)
	service.cached("cluster", build)

	if runs != 1 {
		t.Fatalf("the search ran %d times for one cluster", runs)
	}
}

func lastProbe(t *testing.T, out search) domain.OwnershipProbe {
	t.Helper()
	if len(out.probes) == 0 {
		t.Fatal("the search recorded no probe, so nothing can explain the refusal")
	}
	return out.probes[len(out.probes)-1]
}

func names(apps []*unstructured.Unstructured) []string {
	out := make([]string, 0, len(apps))
	for _, app := range apps {
		out = append(out, app.GetNamespace()+"/"+app.GetName())
	}
	return out
}

// fakeCluster builds a client serving the given Applications, and a way to read
// back what was asked of it.
func fakeCluster(apps ...*unstructured.Unstructured) (*kube.ClusterClient, func() []k8stesting.ListAction) {
	scheme := runtime.NewScheme()
	scheme.AddKnownTypeWithName(
		schema.GroupVersionKind{Group: "argoproj.io", Version: "v1alpha1", Kind: "ApplicationList"},
		&unstructured.UnstructuredList{})

	objects := make([]runtime.Object, 0, len(apps))
	for _, app := range apps {
		objects = append(objects, app)
	}
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{applicationGVR: "ApplicationList"}, objects...)

	read := func() []k8stesting.ListAction {
		var out []k8stesting.ListAction
		for _, action := range client.Actions() {
			if list, ok := action.(k8stesting.ListAction); ok {
				out = append(out, list)
			}
		}
		return out
	}
	return &kube.ClusterClient{Dynamic: client}, read
}
