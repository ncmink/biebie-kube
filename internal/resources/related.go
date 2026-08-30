package resources

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// revisionAnnotation is where a deployment records which rollout a replica set
// belongs to.
const revisionAnnotation = "deployment.kubernetes.io/revision"

// relatedBudget bounds one group.
//
// A related list answers "what is this object running?", which a person reads;
// past a couple of hundred rows they stop reading and start filtering, and the
// pods page is the better tool for that.
const relatedBudget = 200

// ownerKinds maps an ownerReference's Kind to a catalogue entry.
//
// Only the controllers this application knows how to show. An object owned by
// an operator's custom resource — an Argo Rollout, a Flux Kustomization — is
// left out rather than guessed at: naming a kind the navigation cannot open
// would offer the user a link to nowhere.
var ownerKinds = map[string]domain.Kind{
	"CronJob":               domain.KindCronJob,
	"DaemonSet":             domain.KindDaemonSet,
	"Deployment":            domain.KindDeployment,
	"Job":                   domain.KindJob,
	"Node":                  domain.KindNode,
	"ReplicaSet":            domain.KindReplicaSet,
	"ReplicationController": domain.KindReplicationController,
	"StatefulSet":           domain.KindStatefulSet,
}

// Related lists the objects that belong to one object: the pods a deployment
// runs, the replica sets behind its revisions, the workload a pod came from.
//
// Labels narrow the read and ownership decides the answer, which is how a
// controller finds its own children. Two deployments in one namespace can
// carry the same `app` label, so a label match alone would answer "which pods
// is this deployment running?" with the other one's pods as well — the sort of
// wrong that gets acted on. A UID cannot be shared, but neither can it be
// asked for: `metadata.ownerReferences` is not an indexed field, so the
// selector is what keeps the read from being the whole namespace.
func (s *Service) Related(ctx context.Context, clusterID string, ref domain.ResourceRef) ([]domain.RelatedGroup, error) {
	obj, err := s.Get(ctx, clusterID, ref)
	if err != nil {
		return nil, err
	}

	var groups []domain.RelatedGroup
	if owner, ok := s.ownerGroup(ctx, clusterID, obj); ok {
		groups = append(groups, owner)
	}

	switch ref.Kind {
	case domain.KindDeployment:
		// A deployment owns replica sets, and the replica sets own the pods.
		// Both hops are worth showing: the revisions say what happened to the
		// rollout, the pods say what is serving traffic now.
		//
		// One selector narrows both reads. A deployment's replica sets and its
		// pods all carry the pod template's labels, which is what its selector
		// is written against.
		selector := narrowingSelector(obj)
		replicaSets, err := s.ownedObjects(ctx, clusterID, domain.KindReplicaSet, obj, selector)
		if err != nil {
			return nil, err
		}
		if group, ok := s.revisionGroup(clusterID, replicaSets); ok {
			groups = append(groups, group)
		}
		if group, ok := s.podsOwnedBy(ctx, clusterID, obj.GetNamespace(), selector, uidsOf(replicaSets)); ok {
			groups = append(groups, group)
		}

	case domain.KindStatefulSet, domain.KindDaemonSet, domain.KindReplicaSet,
		domain.KindReplicationController, domain.KindJob:
		owners := uidsOf([]*unstructured.Unstructured{obj})
		if group, ok := s.podsOwnedBy(ctx, clusterID, obj.GetNamespace(), narrowingSelector(obj), owners); ok {
			groups = append(groups, group)
		}

	case domain.KindCronJob:
		// A cron job has no selector of its own and stamps no label of its own
		// on the jobs it creates, so this read is the namespace. Jobs are few
		// where pods are many, which is why that is affordable here and would
		// not be for a workload.
		jobs, err := s.ownedObjects(ctx, clusterID, domain.KindJob, obj, labels.Everything())
		if err != nil {
			return nil, err
		}
		if group, ok := s.group(clusterID, domain.KindJob, "Jobs", jobs); ok {
			groups = append(groups, group)
		}

	case domain.KindService:
		// A service is the one relationship Kubernetes really does express
		// with labels: it routes to whatever carries them, whoever owns it.
		selector, ok := serviceSelector(obj)
		if !ok {
			break
		}
		if group, ok := s.podsMatching(ctx, clusterID, obj.GetNamespace(), selector); ok {
			groups = append(groups, group)
		}

	case domain.KindNode:
		group, err := s.podsOnNode(ctx, clusterID, obj.GetName())
		if err != nil {
			return nil, err
		}
		if len(group.Rows) > 0 {
			groups = append(groups, group)
		}
	}

	return groups, nil
}

// ownerGroup names the controller that created an object, following the chain
// up: a pod names both its replica set and the deployment behind it, because
// the deployment is the one an engineer is going to act on.
func (s *Service) ownerGroup(ctx context.Context, clusterID string, obj *unstructured.Unstructured) (domain.RelatedGroup, bool) {
	var rows []domain.ResourceRow
	var kind domain.Kind

	for hop := 0; hop < 2; hop++ {
		reference, ok := controllerOf(obj)
		if !ok {
			break
		}
		ownerKind, known := ownerKinds[reference.Kind]
		if !known {
			break
		}
		info, ok := s.clusters.LookupKind(clusterID, ownerKind)
		if !ok {
			break
		}
		namespace := obj.GetNamespace()
		if !info.Namespaced {
			namespace = ""
		}
		owner, err := s.Get(ctx, clusterID, domain.ResourceRef{
			Kind:      ownerKind,
			Namespace: namespace,
			Name:      reference.Name,
		})
		if err != nil {
			break
		}
		// A stale ownerReference points at a name that has since been
		// recreated. The UID says whether this is the object that made ours.
		if string(owner.GetUID()) != string(reference.UID) {
			break
		}
		rows = append(rows, Row(info, owner))
		kind = ownerKind
		obj = owner
	}

	if len(rows) == 0 {
		return domain.RelatedGroup{}, false
	}
	// The columns belong to the last owner read; a chain that mixes kinds is
	// shown by name alone rather than with cells that mean different things in
	// different rows.
	info, _ := s.clusters.LookupKind(clusterID, kind)
	columns := info.Columns
	if len(rows) > 1 {
		columns = nil
	}
	return domain.RelatedGroup{
		Kind:    kind,
		Title:   "Controlled By",
		Columns: columns,
		Rows:    rows,
	}, true
}

// ownedObjects lists the objects of one kind in an owner's namespace that the
// owner created, reading only the ones the selector could match.
func (s *Service) ownedObjects(
	ctx context.Context,
	clusterID string,
	kind domain.Kind,
	owner *unstructured.Unstructured,
	selector labels.Selector,
) ([]*unstructured.Unstructured, error) {
	info, ok := s.clusters.LookupKind(clusterID, kind)
	if !ok {
		return nil, fmt.Errorf("unknown resource type %q", kind)
	}
	objects, _, err := s.readMatching(ctx, clusterID, info, owner.GetNamespace(), selector)
	if err != nil {
		return nil, err
	}

	uid := string(owner.GetUID())
	var owned []*unstructured.Unstructured
	for _, object := range objects {
		if reference, ok := controllerOf(object); ok && string(reference.UID) == uid {
			owned = append(owned, object)
		}
	}
	return owned, nil
}

// podsOwnedBy lists the pods created by any of a set of controllers.
func (s *Service) podsOwnedBy(
	ctx context.Context,
	clusterID, namespace string,
	selector labels.Selector,
	owners map[string]bool,
) (domain.RelatedGroup, bool) {
	if len(owners) == 0 {
		return domain.RelatedGroup{}, false
	}
	return s.podGroup(ctx, clusterID, namespace, selector, func(pod *unstructured.Unstructured) bool {
		reference, ok := controllerOf(pod)
		return ok && owners[string(reference.UID)]
	})
}

// podsMatching lists the pods in a namespace a service's selector accepts.
//
// Here the selector is the answer rather than a way of narrowing the read, so
// nothing is filtered afterwards: a service routes to whatever carries its
// labels, whoever owns it.
func (s *Service) podsMatching(
	ctx context.Context,
	clusterID, namespace string,
	selector labels.Selector,
) (domain.RelatedGroup, bool) {
	return s.podGroup(ctx, clusterID, namespace, selector, func(*unstructured.Unstructured) bool {
		return true
	})
}

// podGroup renders the pods in a namespace that a selector could match and a
// test accepts.
//
// The two stages do different jobs. The selector is an optimisation the API
// server can help with; the test is the correctness. Keeping them apart is
// what lets a kind whose selector cannot be read fall back to reading the
// namespace instead of answering with it.
func (s *Service) podGroup(
	ctx context.Context,
	clusterID, namespace string,
	selector labels.Selector,
	keep func(*unstructured.Unstructured) bool,
) (domain.RelatedGroup, bool) {
	info, ok := s.clusters.LookupKind(clusterID, domain.KindPod)
	if !ok {
		return domain.RelatedGroup{}, false
	}
	pods, truncated, err := s.readMatching(ctx, clusterID, info, namespace, selector)
	if err != nil {
		return domain.RelatedGroup{}, false
	}

	var matched []*unstructured.Unstructured
	for _, pod := range pods {
		if keep(pod) {
			matched = append(matched, pod)
		}
	}
	if len(matched) == 0 {
		return domain.RelatedGroup{}, false
	}

	group := s.renderPods(ctx, clusterID, info, matched)
	// A read that stopped at the budget is a prefix whether or not the group
	// itself hit one, and saying so is the difference between a short list and
	// a wrong one.
	group.Truncated = group.Truncated || truncated
	return group, true
}

// podsOnNode lists what a node is running.
//
// A field selector rather than a scan: this is the one related list that spans
// every namespace, and asking the API server to do the filtering is the
// difference between one node's pods and the whole cluster's crossing the wire.
func (s *Service) podsOnNode(ctx context.Context, clusterID, node string) (domain.RelatedGroup, error) {
	info, ok := s.clusters.LookupKind(clusterID, domain.KindPod)
	if !ok {
		return domain.RelatedGroup{}, fmt.Errorf("unknown resource type %q", domain.KindPod)
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.RelatedGroup{}, err
	}

	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)
	list, err := client.Dynamic.Resource(gvr).List(ctx, metav1.ListOptions{
		FieldSelector: "spec.nodeName=" + node,
		Limit:         relatedBudget,
	})
	if err != nil {
		return domain.RelatedGroup{}, fmt.Errorf("list pods on %s: %w", node, err)
	}

	pods := make([]*unstructured.Unstructured, 0, len(list.Items))
	for i := range list.Items {
		pods = append(pods, &list.Items[i])
	}

	group := s.renderPods(ctx, clusterID, info, pods)
	group.Namespaced = true
	group.Truncated = group.Truncated || list.GetContinue() != ""
	return group, nil
}

// renderPods turns pods into a group, carrying the same usage columns the pods
// table shows so the two views agree about what a pod is costing.
func (s *Service) renderPods(
	ctx context.Context,
	clusterID string,
	info domain.KindInfo,
	pods []*unstructured.Unstructured,
) domain.RelatedGroup {
	usage := s.usageFor(ctx, clusterID, true)

	truncated := false
	if len(pods) > relatedBudget {
		pods, truncated = pods[:relatedBudget], true
	}

	rows := make([]domain.ResourceRow, 0, len(pods))
	for _, pod := range pods {
		row := Row(info, pod)
		withUsage(&row, usage[row.Key])
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Key < rows[j].Key })

	return domain.RelatedGroup{
		Kind:      domain.KindPod,
		Title:     "Pods",
		Columns:   info.Columns,
		Rows:      rows,
		Truncated: truncated,
	}
}

// revisionGroup renders a deployment's replica sets as its rollout history,
// newest first, led by the revision number the deployment stamped on them.
func (s *Service) revisionGroup(clusterID string, replicaSets []*unstructured.Unstructured) (domain.RelatedGroup, bool) {
	info, ok := s.clusters.LookupKind(clusterID, domain.KindReplicaSet)
	if !ok || len(replicaSets) == 0 {
		return domain.RelatedGroup{}, false
	}

	sort.Slice(replicaSets, func(i, j int) bool {
		return revisionOf(replicaSets[i]) > revisionOf(replicaSets[j])
	})

	rows := make([]domain.ResourceRow, 0, len(replicaSets))
	for _, replicaSet := range replicaSets {
		row := Row(info, replicaSet)
		if revision := replicaSet.GetAnnotations()[revisionAnnotation]; revision != "" {
			// Row's Fields map is freshly built per row, so writing the extra
			// cell here touches nothing the table holds.
			if row.Fields == nil {
				row.Fields = map[string]string{}
			}
			row.Fields["revision"] = revision
		}
		rows = append(rows, row)
	}

	return domain.RelatedGroup{
		Kind:    domain.KindReplicaSet,
		Title:   "Deploy Revisions",
		Columns: append([]domain.Column{{Key: "revision", Title: "#", Mono: true}}, info.Columns...),
		Rows:    rows,
	}, true
}

// group renders objects of one kind with the catalogue's own columns.
func (s *Service) group(
	clusterID string,
	kind domain.Kind,
	title string,
	objects []*unstructured.Unstructured,
) (domain.RelatedGroup, bool) {
	info, ok := s.clusters.LookupKind(clusterID, kind)
	if !ok || len(objects) == 0 {
		return domain.RelatedGroup{}, false
	}
	if len(objects) > relatedBudget {
		objects = objects[:relatedBudget]
	}

	rows := make([]domain.ResourceRow, 0, len(objects))
	for _, object := range objects {
		rows = append(rows, Row(info, object))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })

	return domain.RelatedGroup{
		Kind:    kind,
		Title:   title,
		Columns: info.Columns,
		Rows:    rows,
	}, true
}

// controllerOf returns the ownerReference marked as the controller.
//
// An object can carry several owners but only one controller, and it is the
// controller that decides what the object is: a pod adopted by a replica set
// belongs to the deployment above it, whatever else also references it.
func controllerOf(obj *unstructured.Unstructured) (metav1.OwnerReference, bool) {
	for _, reference := range obj.GetOwnerReferences() {
		if reference.Controller != nil && *reference.Controller {
			return reference, true
		}
	}
	return metav1.OwnerReference{}, false
}

func uidsOf(objects []*unstructured.Unstructured) map[string]bool {
	uids := make(map[string]bool, len(objects))
	for _, object := range objects {
		uids[string(object.GetUID())] = true
	}
	return uids
}

// narrowingSelector reads the labels a controller uses to find its own
// children, so the API server can make the first pass.
//
// Everything is the honest answer when there is no selector to read, because
// this only ever decides how much comes back: what an object owns is settled
// by UID afterwards. That is also why it does not share code with
// serviceSelector, which refuses the empty case — the two look alike and mean
// opposite things. An unusable selector here is a larger read; an unusable
// selector there would be a headless service claiming the namespace.
func narrowingSelector(obj *unstructured.Unstructured) labels.Selector {
	// A replication controller selects with a flat map, where every other
	// workload uses a LabelSelector. NestedStringMap refuses the latter — its
	// values are a map and a list, not strings — which is what tells the two
	// apart without reading the kind.
	if flat, found, err := unstructured.NestedStringMap(obj.Object, "spec", "selector"); err == nil && found {
		if len(flat) == 0 {
			return labels.Everything()
		}
		return labels.SelectorFromSet(flat)
	}

	raw, found, err := unstructured.NestedMap(obj.Object, "spec", "selector")
	if err != nil || !found {
		return labels.Everything()
	}
	var selector metav1.LabelSelector
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(raw, &selector); err != nil {
		return labels.Everything()
	}
	parsed, err := metav1.LabelSelectorAsSelector(&selector)
	if err != nil {
		return labels.Everything()
	}
	return parsed
}

// serviceSelector reads a service's selector, which is a flat map rather than
// the LabelSelector every workload uses.
//
// A service without one — headless, or backed by hand-written endpoints —
// selects nothing rather than everything. This is the one place a selector is
// the answer rather than a way of narrowing a read, so the empty case has to
// be refused instead of widened.
func serviceSelector(obj *unstructured.Unstructured) (labels.Selector, bool) {
	set, found, err := unstructured.NestedStringMap(obj.Object, "spec", "selector")
	if err != nil || !found || len(set) == 0 {
		return nil, false
	}
	return labels.SelectorFromSet(set), true
}

func revisionOf(obj *unstructured.Unstructured) int {
	revision, err := strconv.Atoi(obj.GetAnnotations()[revisionAnnotation])
	if err != nil {
		return 0
	}
	return revision
}
