package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

// restartedAt is the annotation kubectl stamps on a pod template to roll a
// workload.
//
// The same key is written here on purpose. A restart from this window and one
// from a terminal are then the same operation on the same field, rather than
// two conventions competing over one template.
const restartedAt = "kubectl.kubernetes.io/restartedAt"

// instantiate marks a job created by hand from a cron job, the way
// `kubectl create job --from` marks one.
const instantiate = "cronjob.kubernetes.io/instantiate"

// maxJobName is what a job may be called.
//
// The limit is 63 rather than the 253 of a DNS subdomain because the job's
// name is also copied into a label on every pod it creates, and a label value
// stops at 63.
const maxJobName = 63

// Perform applies one action to one object.
//
// Nothing is written to the object's manifest: every action here is the patch
// or the create that kubectl would issue for the same verb, so a cluster never
// ends up in a state only this application knows how to read.
//
// No refresh follows. The watch behind the table sees the change the same way
// it sees one made from anywhere else, which is what keeps a scale from this
// window and a scale from a colleague's terminal indistinguishable on screen.
func (s *Service) Perform(
	ctx context.Context,
	clusterID string,
	request domain.ActionRequest,
) (domain.ActionResult, error) {
	info, ok := s.clusters.LookupKind(clusterID, request.Ref.Kind)
	if !ok {
		return domain.ActionResult{}, fmt.Errorf("unknown resource type %q", request.Ref.Kind)
	}
	if !info.Supports(request.Action) {
		return domain.ActionResult{}, fmt.Errorf(
			"%s does not support the %s action", info.Title, request.Action)
	}
	client, err := s.clusters.Client(clusterID)
	if err != nil {
		return domain.ActionResult{}, err
	}

	var result domain.ActionResult
	switch request.Action {
	case domain.ActionScale:
		result, err = scale(ctx, client.Dynamic, info, request)
	case domain.ActionRestart:
		result, err = restart(ctx, client.Dynamic, info, request, time.Now())
	case domain.ActionCordon, domain.ActionUncordon:
		result, err = cordon(ctx, client.Dynamic, info, request)
	case domain.ActionSuspend, domain.ActionResume:
		result, err = suspend(ctx, client.Dynamic, info, request)
	case domain.ActionTrigger:
		result, err = trigger(ctx, client.Dynamic, info, request, time.Now())
	default:
		return domain.ActionResult{}, fmt.Errorf("unknown action %q", request.Action)
	}
	if err != nil {
		return domain.ActionResult{}, describeActionError(err, request)
	}
	return result, nil
}

// scoped narrows the dynamic client to where one object lives.
func scoped(client dynamic.Interface, info domain.KindInfo, namespace string) dynamic.ResourceInterface {
	gvr := kube.GVRFor(info.Group, info.Version, info.Resource)
	if info.Namespaced {
		return client.Resource(gvr).Namespace(namespace)
	}
	return client.Resource(gvr)
}

// scale writes through the scale subresource rather than through the object.
//
// Every scalable kind exposes that subresource with the same shape, so one
// patch serves deployments, stateful sets and replication controllers without
// a branch per kind — and it is the endpoint `kubectl scale` uses, so RBAC
// written for one covers the other.
func scale(
	ctx context.Context,
	client dynamic.Interface,
	info domain.KindInfo,
	request domain.ActionRequest,
) (domain.ActionResult, error) {
	if request.Replicas < 0 {
		return domain.ActionResult{}, fmt.Errorf("a replica count cannot be negative")
	}
	patch, err := marshal(map[string]any{
		"spec": map[string]any{"replicas": request.Replicas},
	})
	if err != nil {
		return domain.ActionResult{}, err
	}

	_, err = scoped(client, info, request.Ref.Namespace).
		Patch(ctx, request.Ref.Name, types.MergePatchType, patch, metav1.PatchOptions{}, "scale")
	if err != nil {
		return domain.ActionResult{}, err
	}
	return domain.ActionResult{
		Message: fmt.Sprintf("Scaled %s to %d.", request.Ref.Name, request.Replicas),
	}, nil
}

// restart rolls a workload by changing its pod template and nothing else.
func restart(
	ctx context.Context,
	client dynamic.Interface,
	info domain.KindInfo,
	request domain.ActionRequest,
	now time.Time,
) (domain.ActionResult, error) {
	patch, err := restartPatch(now)
	if err != nil {
		return domain.ActionResult{}, err
	}

	_, err = scoped(client, info, request.Ref.Namespace).
		Patch(ctx, request.Ref.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return domain.ActionResult{}, err
	}
	return domain.ActionResult{
		Message: fmt.Sprintf("Restarting %s.", request.Ref.Name),
	}, nil
}

// restartPatch stamps a pod template with a timestamp.
//
// That stamp is the whole of a rollout restart: the template differs from the
// one the controller last acted on, so it replaces the pods under whatever
// update strategy the workload declares. Nothing here decides how fast that
// happens.
func restartPatch(at time.Time) ([]byte, error) {
	return marshal(map[string]any{
		"spec": map[string]any{
			"template": map[string]any{
				"metadata": map[string]any{
					"annotations": map[string]any{
						restartedAt: at.UTC().Format(time.RFC3339),
					},
				},
			},
		},
	})
}

// cordon takes a node in or out of the scheduler's reach.
func cordon(
	ctx context.Context,
	client dynamic.Interface,
	info domain.KindInfo,
	request domain.ActionRequest,
) (domain.ActionResult, error) {
	unschedulable := request.Action == domain.ActionCordon
	patch, err := marshal(map[string]any{
		"spec": map[string]any{"unschedulable": unschedulable},
	})
	if err != nil {
		return domain.ActionResult{}, err
	}

	_, err = scoped(client, info, request.Ref.Namespace).
		Patch(ctx, request.Ref.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return domain.ActionResult{}, err
	}

	// Cordoning is half a drain, and saying so is the difference between an
	// engineer who waits for the node to empty and one who knows it will not.
	if unschedulable {
		return domain.ActionResult{
			Message: fmt.Sprintf("Cordoned %s. Its pods keep running.", request.Ref.Name),
		}, nil
	}
	return domain.ActionResult{
		Message: fmt.Sprintf("Uncordoned %s.", request.Ref.Name),
	}, nil
}

// suspend stops or restarts a cron job's schedule.
func suspend(
	ctx context.Context,
	client dynamic.Interface,
	info domain.KindInfo,
	request domain.ActionRequest,
) (domain.ActionResult, error) {
	suspended := request.Action == domain.ActionSuspend
	patch, err := marshal(map[string]any{
		"spec": map[string]any{"suspend": suspended},
	})
	if err != nil {
		return domain.ActionResult{}, err
	}

	_, err = scoped(client, info, request.Ref.Namespace).
		Patch(ctx, request.Ref.Name, types.MergePatchType, patch, metav1.PatchOptions{})
	if err != nil {
		return domain.ActionResult{}, err
	}

	if suspended {
		return domain.ActionResult{
			Message: fmt.Sprintf("Suspended %s. Runs already started are left alone.", request.Ref.Name),
		}, nil
	}
	return domain.ActionResult{
		Message: fmt.Sprintf("Resumed %s.", request.Ref.Name),
	}, nil
}

// trigger runs a cron job now, by creating the job its schedule would create.
func trigger(
	ctx context.Context,
	client dynamic.Interface,
	info domain.KindInfo,
	request domain.ActionRequest,
	now time.Time,
) (domain.ActionResult, error) {
	jobKind, ok := domain.Lookup(domain.KindJob)
	if !ok {
		return domain.ActionResult{}, fmt.Errorf("jobs are not in the catalogue")
	}

	cronJob, err := scoped(client, info, request.Ref.Namespace).
		Get(ctx, request.Ref.Name, metav1.GetOptions{})
	if err != nil {
		return domain.ActionResult{}, err
	}

	job, err := jobFromCronJob(cronJob, jobKind, now)
	if err != nil {
		return domain.ActionResult{}, err
	}

	created, err := scoped(client, jobKind, job.GetNamespace()).
		Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return domain.ActionResult{}, err
	}
	return domain.ActionResult{
		Message: fmt.Sprintf("Created job %s.", created.GetName()),
	}, nil
}

// jobFromCronJob builds the job a cron job's next scheduled run would create.
//
// The job is deliberately left unowned. An owner reference would enrol a manual
// run in the cron job's own bookkeeping — counted against its history limits,
// and able to block the next scheduled run under a Forbid concurrency policy —
// and a run somebody asked for by hand is not a run the schedule produced.
// This is why `kubectl create job --from` leaves it unowned too.
func jobFromCronJob(
	cronJob *unstructured.Unstructured,
	jobKind domain.KindInfo,
	at time.Time,
) (*unstructured.Unstructured, error) {
	template, found, err := unstructured.NestedMap(cronJob.Object, "spec", "jobTemplate")
	if err != nil || !found {
		return nil, fmt.Errorf("cron job %s carries no job template", cronJob.GetName())
	}
	spec, found, err := unstructured.NestedMap(template, "spec")
	if err != nil || !found {
		return nil, fmt.Errorf("cron job %s carries no job template", cronJob.GetName())
	}

	// The template's own metadata comes along: the labels a cron job stamps on
	// its jobs are how everything downstream — a dashboard, an alert — knows
	// what the run belongs to, and a manual run that dropped them would be
	// invisible to all of it.
	metadata, _, _ := unstructured.NestedMap(template, "metadata")
	if metadata == nil {
		metadata = map[string]any{}
	}

	job := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": apiVersion(jobKind),
		"kind":       "Job",
		"metadata":   metadata,
		"spec":       spec,
	}}
	job.SetNamespace(cronJob.GetNamespace())
	job.SetName(manualJobName(cronJob.GetName(), at))
	job.SetOwnerReferences(nil)

	annotations := job.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[instantiate] = "manual"
	job.SetAnnotations(annotations)

	return job, nil
}

// manualJobName names a run so it reads apart from a scheduled one at a
// glance, and still fits in what a job may be called.
func manualJobName(cronJob string, at time.Time) string {
	suffix := fmt.Sprintf("-manual-%d", at.Unix())
	base := cronJob
	if room := maxJobName - len(suffix); len(base) > room {
		base = strings.TrimRight(base[:room], "-")
	}
	return base + suffix
}

// apiVersion assembles the field a manifest carries, which is the bare version
// for a core kind and group/version for everything else.
func apiVersion(info domain.KindInfo) string {
	if info.Group == "" {
		return info.Version
	}
	return info.Group + "/" + info.Version
}

func marshal(patch map[string]any) ([]byte, error) {
	data, err := json.Marshal(patch)
	if err != nil {
		return nil, fmt.Errorf("build patch: %w", err)
	}
	return data, nil
}

// describeActionError says what the cluster refused, in terms the person who
// clicked can do something about.
func describeActionError(err error, request domain.ActionRequest) error {
	switch {
	case apierrors.IsNotFound(err):
		return fmt.Errorf("%s no longer exists", request.Ref.Name)
	case apierrors.IsForbidden(err):
		return fmt.Errorf("these credentials may not %s %s", request.Action, request.Ref.Name)
	case apierrors.IsConflict(err):
		return fmt.Errorf("%s changed in the cluster while this ran; try again", request.Ref.Name)
	case apierrors.IsAlreadyExists(err):
		return fmt.Errorf("a job for this run already exists; wait a moment and try again")
	default:
		return fmt.Errorf("%s %s: %w", request.Action, request.Ref.Name, err)
	}
}
