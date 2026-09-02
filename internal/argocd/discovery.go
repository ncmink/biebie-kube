package argocd

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// This file is the search that every ownership answer rests on: which Argo CD
// Applications exist in this cluster.
//
// Two properties matter more than anything else it does.
//
// The search is cluster-wide and has nothing to do with what the navigator is
// showing. An Application in `argocd` manages a Deployment in `reporting`, and
// a user who has filtered the resource list down to `reporting` has expressed a
// preference about a table, not a fact about the cluster. Scoping ownership to
// the selected namespace would make the owning Application invisible, report
// the Deployment as unmanaged, and offer a direct write on the strength of a UI
// control. No function in this file takes a UI namespace.
//
// The second is that an incomplete search never produces an absence. A listing
// that was refused, cut short, or narrowed to a single namespace has not
// established that no Application claims anything — it has established nothing
// — and that is carried out of here as an uncertainty rather than as an empty
// slice somebody downstream will read as "none".

// ownershipTimeout bounds one ownership search.
//
// A person is waiting on a drawer, and a permission that is going to be refused
// is refused immediately. This bound exists for the other case: an API server
// that has stopped answering. It is short deliberately — an ownership check
// that takes half a minute to say "unknown" is worse than one that says it
// straight away, because the window in which mutation buttons are disabled and
// unexplained is the window a person spends wondering if the app is broken.
const ownershipTimeout = 6 * time.Second

// pageBudget is how many pages of listBudget the search will read before it
// gives up and calls the result incomplete.
//
// A cluster with more than this many Applications exists, and the honest answer
// for it is "this could not be enumerated" rather than an answer computed from
// the first ten thousand.
const pageBudget = 5

// Cache lifetimes. Ownership is asked on every drawer open and every create
// dialog, and a full Application listing per row would make clicking down a
// list feel broken.
//
// The failure lifetime is longer than the success one on purpose. A refused
// permission is not going to start working within seconds, and re-asking for it
// on every row is exactly the behaviour that leaves non-admin users waiting on
// checks that were never going to pass.
const (
	discoveryTTL = 15 * time.Second
	refusalTTL   = 60 * time.Second
)

// installState is what is known about Argo CD being present.
//
// Three values rather than a boolean, because "the definition is not served"
// and "this account may not read what is served" are different facts and only
// the first is evidence of absence.
type installState int

const (
	// installUnknown is discovery not having answered. It is the zero value
	// so that a search which never got as far as asking cannot be mistaken for
	// one that established absence.
	installUnknown installState = iota
	installYes
	installNo
)

// search is one attempt to see every Application in a cluster.
type search struct {
	apps []*unstructured.Unstructured

	// complete reports that the search space was enumerated in full. Only a
	// complete search may produce "nothing claims this".
	complete bool

	installed installState

	uncertainty domain.OwnershipUncertainty
	reason      string
	probes      []domain.OwnershipProbe
}

// cache holds one search per cluster, and one per namespace fallback.
type cache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry

	// now is the clock, so the lifetimes above can be tested without a test
	// that sleeps for a minute.
	now func() time.Time
}

type cacheEntry struct {
	result  search
	expires time.Time
}

func (c *cache) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *cache) get(key string) (search, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || c.clock().After(entry.expires) {
		return search{}, false
	}
	return entry.result, true
}

func (c *cache) put(key string, result search) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]cacheEntry)
	}
	c.entries[key] = cacheEntry{result: result, expires: c.clock().Add(ttlFor(result))}
}

// ttlFor is how long one search answer is worth reusing.
//
// A refusal is held longer than a success on purpose. RBAC does not change
// while somebody clicks down a list, and re-asking for a permission that was
// just denied — once per row, for every row — is exactly the behaviour that
// leaves a non-admin account waiting on checks that were never going to pass.
func ttlFor(result search) time.Duration {
	if result.uncertainty == domain.UncertaintyForbidden {
		return refusalTTL
	}
	return discoveryTTL
}

// discover finds the Applications an ownership question has to be answered
// against.
//
// fallback is the target object's own namespace and is not a filter. It is used
// only when the cluster-wide read was refused: an installation running
// "applications in any namespace" may keep the owning Application beside the
// object, and an account that may read that one namespace can still find a
// positive claim there. Finding nothing in it proves nothing, which is why the
// result stays incomplete either way.
func (s *Service) discover(ctx context.Context, clusterID, fallback string) search {
	wide := s.cached(clusterID, func() search { return s.searchCluster(ctx, clusterID) })
	if wide.complete || fallback == "" || wide.installed == installNo {
		return wide
	}
	return s.cached(clusterID+"\x00"+fallback, func() search {
		return s.searchNamespace(ctx, clusterID, fallback, wide)
	})
}

func (s *Service) cached(key string, build func() search) search {
	if found, ok := s.cache.get(key); ok {
		return found
	}
	result := build()
	s.cache.put(key, result)
	return result
}

// searchCluster lists every Application in the cluster.
func (s *Service) searchCluster(ctx context.Context, clusterID string) search {
	out := search{installed: s.installation(clusterID)}

	if out.installed == installNo {
		// Positively established absence: the cluster told us what it serves
		// and the Application definition was not among it. This is the one
		// path on which "nothing is managed here" is a fact rather than a
		// silence, and it is why the check is worth having.
		out.complete = true
		out.reason = "Argo CD is not installed in this cluster, so nothing here is managed from Git."
		out.probes = append(out.probes, probe("cluster", "", domain.OwnershipProbeNotInstalled,
			"This cluster does not serve the Argo CD Application resource."))
		return out
	}

	client, err := s.clusters.Client(clusterID)
	if err != nil {
		out.uncertainty = domain.UncertaintyUnreachable
		out.reason = "This cluster is not connected, so Argo CD ownership could not be checked."
		out.probes = append(out.probes, probe("cluster", "", domain.OwnershipProbeFailed, "This cluster is not connected."))
		return out
	}

	ctx, cancel := context.WithTimeout(ctx, ownershipTimeout)
	defer cancel()

	apps, truncated, err := listPaged(ctx, client, applicationGVR, "")
	if err != nil {
		return refused(out, "cluster", "", err)
	}

	// A listing that answered is the strongest possible evidence that Argo CD
	// is here, and it outranks a discovery read that could not be made.
	out.installed = installYes
	out.apps = apps

	if truncated {
		out.uncertainty = domain.UncertaintyIncomplete
		out.reason = "This cluster has more Argo CD Applications than Biebie Kube reads in one pass, " +
			"so whether one of the rest claims this target is unknown."
		out.probes = append(out.probes, probe("cluster", "", domain.OwnershipProbeTruncated,
			"The Application listing was cut short by the page budget."))
		return out
	}

	out.complete = true
	out.probes = append(out.probes, probe("cluster", "", domain.OwnershipProbeOK, ""))
	return out
}

// searchNamespace narrows a refused cluster-wide search to one namespace.
//
// The result stays incomplete whatever it finds. One namespace of a cluster is
// not the cluster, and the only thing this can honestly upgrade is a positive:
// an Application found here claims what it claims regardless of what could not
// be read elsewhere.
func (s *Service) searchNamespace(ctx context.Context, clusterID, namespace string, wide search) search {
	out := wide

	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return out
	}

	ctx, cancel := context.WithTimeout(ctx, ownershipTimeout)
	defer cancel()

	apps, truncated, err := listPaged(ctx, client, applicationGVR, namespace)
	if err != nil {
		out.probes = append(out.probes, probeFor("namespace", namespace, err))
		return out
	}

	out.apps = append(out.apps, apps...)
	out.installed = installYes

	result := domain.OwnershipProbeOK
	if truncated {
		result = domain.OwnershipProbeTruncated
	}
	out.probes = append(out.probes, probe("namespace", namespace, result, ""))
	return out
}

// installation reports what is known about Argo CD being served here.
//
// Two sources, in order of how much they establish. The session catalogue is
// built from the cluster's CustomResourceDefinitions, which a namespace-scoped
// account frequently may not list — so its silence proves nothing. API
// discovery is readable by any authenticated account and is what turns "not in
// the catalogue" into "not served by this cluster".
//
// Neither is a hardcoded namespace. Where Argo CD was installed is not asked
// and does not matter: what matters is whether the API serves the resource.
func (s *Service) installation(clusterID string) installState {
	if _, ok := s.clusters.LookupKind(clusterID, domain.KindArgoApplication); ok {
		return installYes
	}

	served := s.clusters.APIResources(clusterID)
	if len(served) == 0 {
		// Discovery returned nothing, which is a failed read rather than an
		// empty cluster. Reading it as "Argo CD is absent" is how a cluster
		// with a sick aggregated API server starts offering direct writes.
		return installUnknown
	}
	if kube.Supports(served, applicationGVR) {
		return installYes
	}
	return installNo
}

// listPaged reads one resource type through to the end, or to the budget.
//
// The continue token is followed rather than ignored. A single List with a
// limit returns a prefix and says so, and treating that prefix as the whole
// cluster is the quietest way to conclude that nothing claims an object.
func listPaged(
	ctx context.Context,
	client *kube.ClusterClient,
	gvr schema.GroupVersionResource,
	namespace string,
) ([]*unstructured.Unstructured, bool, error) {
	var api dynamic.ResourceInterface = client.Dynamic.Resource(gvr)
	if namespace != "" {
		api = client.Dynamic.Resource(gvr).Namespace(namespace)
	}

	out := make([]*unstructured.Unstructured, 0, listBudget)
	token := ""
	for page := 0; page < pageBudget; page++ {
		list, err := api.List(ctx, metav1.ListOptions{Limit: listBudget, Continue: token})
		if err != nil {
			return nil, false, err
		}
		for i := range list.Items {
			out = append(out, &list.Items[i])
		}
		if token = list.GetContinue(); token == "" {
			return out, false, nil
		}
	}
	return out, true, nil
}

// refused turns a failed Application listing into a search that says so.
//
// A 404 is the interesting case and it is not a failure: the dynamic client
// answers that way for a resource the API server does not serve, which
// establishes absence as firmly as discovery does.
func refused(out search, scope, namespace string, err error) search {
	if apierrors.IsNotFound(err) {
		out.installed = installNo
		out.complete = true
		out.reason = "Argo CD is not installed in this cluster, so nothing here is managed from Git."
		out.probes = append(out.probes, probe(scope, namespace, domain.OwnershipProbeNotInstalled,
			"This cluster does not serve the Argo CD Application resource."))
		return out
	}

	found := probeFor(scope, namespace, err)
	out.probes = append(out.probes, found)

	switch found.Result {
	case domain.OwnershipProbeForbidden:
		out.uncertainty = domain.UncertaintyForbidden
		out.reason = "This account may not list Argo CD Applications, so whether one claims this target could not be checked."
	case domain.OwnershipProbeTimeout:
		out.uncertainty = domain.UncertaintyTimeout
		out.reason = "Argo CD Applications could not be listed in time, so whether one claims this target is unknown."
	default:
		out.uncertainty = domain.UncertaintyUnreachable
		out.reason = "Argo CD Applications could not be listed, so whether one claims this target is unknown."
	}
	return out
}

// probeFor records one failed request in the terms RBAC is written in.
func probeFor(scope, namespace string, err error) domain.OwnershipProbe {
	switch {
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return probe(scope, namespace, domain.OwnershipProbeForbidden, oneLine(err))
	case errors.Is(err, context.DeadlineExceeded), apierrors.IsTimeout(err), apierrors.IsServerTimeout(err):
		return probe(scope, namespace, domain.OwnershipProbeTimeout, "The request did not complete within "+ownershipTimeout.String()+".")
	default:
		return probe(scope, namespace, domain.OwnershipProbeFailed, oneLine(err))
	}
}

func probe(scope, namespace string, result domain.OwnershipProbeResult, detail string) domain.OwnershipProbe {
	return domain.OwnershipProbe{
		Resource:  "applications.argoproj.io",
		Verb:      "list",
		Scope:     scope,
		Namespace: namespace,
		Result:    result,
		Detail:    detail,
	}
}

// oneLine reduces an API error to its first line.
//
// Kubernetes forbidden messages name the account, the verb and the resource,
// which is the whole point of showing one. They carry no credential: the token
// is in the request header and never in the response, and nothing here reads a
// request. The first line is taken because a wrapped error can drag a stack of
// context nobody reading a permission wants.
func oneLine(err error) string {
	if err == nil {
		return ""
	}
	line := strings.TrimSpace(strings.SplitN(err.Error(), "\n", 2)[0])
	if len(line) > 300 {
		line = line[:300] + "…"
	}
	return line
}
