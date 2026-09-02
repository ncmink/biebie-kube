// Package argocd reads the Argo CD installation a cluster happens to have.
//
// Everything here comes from the Kubernetes API through client-go, the same as
// every other view in this application. Argo CD's own REST API is deliberately
// not spoken: it would need a second set of credentials, a reachable server
// and a login this application has no business holding, to read state that is
// already on the objects.
package argocd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
	"biebie-kube/internal/portforward"
)

// The Argo CD custom resources this application reads.
var (
	applicationGVR    = kube.GVRFor("argoproj.io", "v1alpha1", "applications")
	applicationSetGVR = kube.GVRFor("argoproj.io", "v1alpha1", "applicationsets")
	appProjectGVR     = kube.GVRFor("argoproj.io", "v1alpha1", "appprojects")
)

// Argo CD registers a repository and a cluster as a labelled Secret rather
// than as a resource of its own, so those two views are label queries.
const (
	secretTypeLabel = "argocd.argoproj.io/secret-type"

	// serverSelector finds argocd-server wherever it was installed. The
	// namespace is not assumed to be "argocd": a customer who installed the
	// chart somewhere else still gets a working button.
	serverSelector = "app.kubernetes.io/name=argocd-server"
)

// listBudget bounds each read behind the dashboard. The page is a summary, and
// a cluster with more Applications than this has a bigger problem than a
// slightly low count.
const listBudget = 2000

// activityLimit is how many timeline entries cross the binding. The filter in
// the panel searches what it was given, so this is chosen to be more than a
// person scrolls and less than a page that takes a moment to render.
const activityLimit = 100

// Service answers the Argo CD dashboard and runs its two actions.
type Service struct {
	clusters *cluster.Manager
	forwards *portforward.Service

	// cache holds recent Application searches, so clicking down a list of
	// resources does not list every Application in the cluster per row.
	cache cache
}

// NewService wires the service to live cluster sessions.
func NewService(clusters *cluster.Manager, forwards *portforward.Service) *Service {
	return &Service{clusters: clusters, forwards: forwards}
}

// Installed reports whether this cluster serves the Application definition.
//
// Two sources are consulted rather than one. The session catalogue is what the
// sidebar builds from, so the dashboard and the navigation cannot disagree; API
// discovery is the fallback, because the catalogue's custom entries come from
// listing CustomResourceDefinitions and a namespace-scoped account may not do
// that. An account that cannot read definitions was previously indistinguishable
// from a cluster with no Argo CD at all.
//
// The answer is a boolean here because the sidebar only needs to know whether
// to draw an entry. Everything that gates a write goes through the three-state
// installation check instead, which keeps "not served" apart from "not read".
func (s *Service) Installed(clusterID string) bool {
	return s.installation(clusterID) == installYes
}

// Dashboard builds the Argo CD dashboard.
//
// Each read is best-effort and independent. An account that may list
// Applications but not the repository Secrets gets a page with one fewer card,
// which is the truth for it, rather than an error page.
func (s *Service) Dashboard(ctx context.Context, clusterID string) (domain.ArgoDashboard, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.ArgoDashboard{}, err
	}

	out := domain.ArgoDashboard{
		ClusterID: clusterID,
		Installed: s.Installed(clusterID),
	}
	if service, err := serverService(ctx, client); err == nil {
		out.Namespace = service.Namespace
	}

	apps, appsErr := listAll(ctx, client, applicationGVR)
	if appsErr != nil && !out.Installed {
		// Nothing to summarise and nothing to explain: the cluster simply does
		// not run Argo CD, which the page says in one sentence.
		return out, nil
	}

	summary, needsAttention := summarise(apps)
	out.Summary = summary
	out.NeedsAttention = needsAttention

	out.Cards = append(out.Cards, domain.ArgoCard{
		Kind:    domain.KindArgoApplication,
		Title:   "Applications",
		Total:   summary.Applications,
		Healthy: summary.Healthy,
		Leads:   true,
		Chips:   chipsFor(summary),
	})

	if sets, err := listAll(ctx, client, applicationSetGVR); err == nil {
		out.Cards = append(out.Cards, domain.ArgoCard{
			Kind:  domain.KindArgoApplicationSet,
			Title: "Application Sets",
			Total: len(sets),
		})
	}
	if projects, err := listAll(ctx, client, appProjectGVR); err == nil {
		out.Cards = append(out.Cards, domain.ArgoCard{
			Kind:  domain.KindArgoProject,
			Title: "Projects",
			Total: len(projects),
		})
	}

	// Repositories and clusters are Secrets wearing a label, so their cards
	// carry no kind: a Secrets list filtered to neither is not the view the
	// card would be promising.
	if count, err := countSecrets(ctx, client, "repository"); err == nil {
		out.Cards = append(out.Cards, domain.ArgoCard{Title: "Repositories", Total: count})
	}
	if count, err := countSecrets(ctx, client, "cluster"); err == nil {
		out.Cards = append(out.Cards, domain.ArgoCard{Title: "Clusters", Total: count})
	}

	out.Activity = s.activity(ctx, client)
	return out, nil
}

// Applications lists every Application for the sync and refresh dialogs.
//
// The order is the one the dialogs want: a problem first, so the selection a
// bulk action preselects is also the part of the list the engineer can see.
func (s *Service) Applications(ctx context.Context, clusterID string) ([]domain.ArgoApp, error) {
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return nil, err
	}
	objects, err := listAll(ctx, client, applicationGVR)
	if err != nil {
		return nil, fmt.Errorf("list Argo CD applications: %w", err)
	}

	out := make([]domain.ArgoApp, 0, len(objects))
	for _, obj := range objects {
		app := describeApp(obj)
		app.Reason, _ = attention(app, text(obj, "status", "health", "message"))
		out = append(out, app)
	}

	sort.SliceStable(out, func(i, j int) bool {
		left, right := out[i], out[j]
		if severity(left) != severity(right) {
			return severity(left) < severity(right)
		}
		if outOfSync(left) != outOfSync(right) {
			return outOfSync(left)
		}
		if left.Namespace != right.Namespace {
			return left.Namespace < right.Namespace
		}
		return left.Name < right.Name
	})
	return out, nil
}

// activity builds the timeline from Kubernetes events.
//
// Events are the only cluster-side record of what Argo CD did, and they age
// out on the cluster's own schedule, so a quiet timeline means a quiet hour
// rather than a missing feature.
func (s *Service) activity(ctx context.Context, client *kube.ClusterClient) []domain.ArgoActivity {
	list, err := client.Clientset.CoreV1().Events("").List(ctx, metav1.ListOptions{Limit: 500})
	if err != nil {
		return nil
	}

	out := make([]domain.ArgoActivity, 0, activityLimit)
	for i := range list.Items {
		event := list.Items[i]
		if !argoObject(event.InvolvedObject) {
			continue
		}
		out = append(out, domain.ArgoActivity{
			UID:       string(event.UID),
			Category:  categorise(event.Type, event.Reason),
			Reason:    event.Reason,
			Object:    event.InvolvedObject.Kind + "/" + event.InvolvedObject.Name,
			Message:   event.Message,
			Namespace: event.Namespace,
			At:        eventTime(event),
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > activityLimit {
		out = out[:activityLimit]
	}
	return out
}

// argoObject reports whether an event is about something Argo CD owns.
//
// The API group is what decides it. Matching on the kind alone would sweep in
// every other operator's "Application", and this timeline is meant to be one
// product's story rather than the cluster's whole event log.
func argoObject(ref corev1.ObjectReference) bool {
	group, _, _ := strings.Cut(ref.APIVersion, "/")
	return group == "argoproj.io"
}

// eventTime copes with the two event APIs. Events written through
// events.k8s.io fill eventTime and leave lastTimestamp zero, which would sort
// every modern Argo CD event to the bottom of the timeline.
func eventTime(event corev1.Event) time.Time {
	if !event.LastTimestamp.Time.IsZero() {
		return event.LastTimestamp.Time
	}
	if !event.EventTime.Time.IsZero() {
		return event.EventTime.Time
	}
	return event.FirstTimestamp.Time
}

// chipsFor turns the problem counts into the chips the Applications card
// carries. A count of zero gets no chip: an empty chip is a fact nobody needs
// on a card whose point is what is wrong.
func chipsFor(summary domain.ArgoSummary) []domain.ArgoChip {
	candidates := []domain.ArgoChip{
		{Label: "Degraded", Count: summary.Degraded, Health: domain.HealthCritical},
		{Label: "Missing", Count: summary.Missing, Health: domain.HealthWarning},
		{Label: "Out of sync", Count: summary.OutOfSync, Health: domain.HealthWarning},
		{Label: "Progressing", Count: summary.Progressing, Health: domain.HealthProgress},
	}
	var chips []domain.ArgoChip
	for _, chip := range candidates {
		if chip.Count > 0 {
			chips = append(chips, chip)
		}
	}
	return chips
}

// countSecrets counts the Secrets Argo CD tags with one of its own types.
func countSecrets(ctx context.Context, client *kube.ClusterClient, secretType string) (int, error) {
	list, err := client.Clientset.CoreV1().Secrets("").List(ctx, metav1.ListOptions{
		LabelSelector: secretTypeLabel + "=" + secretType,
		Limit:         listBudget,
	})
	if err != nil {
		return 0, err
	}
	return len(list.Items), nil
}

// serverService finds the Argo CD web UI's Service by its label.
func serverService(ctx context.Context, client *kube.ClusterClient) (corev1.Service, error) {
	list, err := client.Clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{
		LabelSelector: serverSelector,
	})
	if err != nil {
		return corev1.Service{}, fmt.Errorf("look for the Argo CD server: %w", err)
	}
	if len(list.Items) == 0 {
		return corev1.Service{}, fmt.Errorf("no Service labelled %s was found in this cluster", serverSelector)
	}
	return list.Items[0], nil
}

// listAll reads one Argo CD resource type across every namespace.
//
// The dashboard's reads come through here and are allowed to be a prefix of a
// very large cluster: a summary that is slightly low is a summary. Ownership
// does not use it, because there the difference between "none" and "none in
// the first page" is the difference between offering a write and refusing one.
func listAll(ctx context.Context, client *kube.ClusterClient, gvr schema.GroupVersionResource) ([]*unstructured.Unstructured, error) {
	out, _, err := listPaged(ctx, client, gvr, "")
	return out, err
}

func outOfSync(app domain.ArgoApp) bool { return normalise(app.Sync) == "outofsync" }
