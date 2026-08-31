package gitops

import (
	"strings"

	"biebie-kube/internal/argocd"
	"biebie-kube/internal/domain"
)

// Classification is the second half of making a comparison readable, and it
// does a different job from normalisation.
//
// Normalisation erases differences that were never really differences. What
// reaches this file genuinely exists in one object and not the other:
// `deployment.kubernetes.io/revision` really is in the cluster and really is
// not in Git, and pretending otherwise would be lying about the object.
//
// So these are kept and explained. A difference somebody's controller owns is
// still a fact about the object, and an engineer who goes looking for why a
// rollout happened will want to see it. It just should not be the first thing
// on screen, and it should not be counted alongside a container image that
// nobody can account for.
//
// The default is Meaningful. Anything this file does not recognise stays in
// front of the reader, because the cost of showing one field too many is a
// moment's reading and the cost of hiding one is a drift nobody notices.

// systemManaged names the fields written by Kubernetes, its controllers, or
// Argo CD after a manifest has been applied.
//
// Each is a path this application can account for. The list is short for the
// same reason the defaults table is: an entry added carelessly hides drift, and
// the failure is silent.
var systemManaged = map[string]string{
	"metadata.annotations.deployment.kubernetes.io/revision": "The Deployment controller writes this when it rolls out a new ReplicaSet.",

	// `kubectl rollout restart` writes this, and so does this application's own
	// restart action. It is the object's record of an operation rather than
	// anything a repository would carry.
	"spec.template.metadata.annotations.kubectl.kubernetes.io/restartedAt": "A rollout restart wrote this timestamp onto the pod template.",
}

// trackingLabels are how Argo CD marks the objects it owns.
//
// The application already knows about `app.kubernetes.io/instance` as its
// default tracking label, and installations configured with a custom key
// commonly use the second. Both are written by Argo CD onto the object after
// applying, so neither is in the repository.
var trackingLabels = []string{argocd.InstanceLabel, "argocd.argoproj.io/instance"}

// classify decides which differences an engineer should look at, and gives the
// rest a sentence saying who wrote them.
func classify(differences []domain.StateDifference) []domain.StateDifference {
	for index := range differences {
		difference := &differences[index]
		difference.Class = domain.DifferenceMeaningful
		difference.Label, difference.Subject = describe(difference.Path)

		// Only a field the cluster has and the source does not can have been
		// written by a controller. A field both sides declare differently is a
		// disagreement whoever wrote it.
		if difference.Kind != domain.DifferenceAddedInLive {
			continue
		}
		if reason, known := systemManaged[difference.Path]; known {
			mark(difference, domain.GroupController, reason)
			continue
		}
		if label, found := strings.CutPrefix(difference.Path, "metadata.labels."); found {
			for _, tracking := range trackingLabels {
				if label == tracking {
					mark(difference, domain.GroupController,
						"Argo CD writes this label onto the objects it tracks.")
					break
				}
			}
		}
	}
	return differences
}

func mark(difference *domain.StateDifference, group domain.DifferenceGroup, reason string) {
	difference.Class = domain.DifferenceSystemManaged
	difference.Group = group
	difference.Reason = reason
}

// describe turns a field path into the words a person would use for it.
//
// It covers the handful of fields people actually come to this panel about. A
// path it does not recognise gets no label and the UI falls back to showing the
// path itself, which is honest and still readable — inventing a friendly name
// for every field in the Kubernetes API is not work this slice needs done.
func describe(path string) (label, subject string) {
	container := within(path, "containers[name=")
	trimmed := path
	if container != "" {
		_, after, _ := strings.Cut(path, "containers[name="+container+"]")
		trimmed = strings.TrimPrefix(after, ".")
	}

	switch {
	case trimmed == "image" && container != "":
		return "Container image", container
	case trimmed == "" && container != "":
		return "Container", container
	case strings.HasPrefix(trimmed, "resources.") && container != "":
		return resourceLabel(trimmed), container
	}

	if name := within(path, "env[name="); name != "" {
		return "Environment variable", name
	}

	switch path {
	case "spec.replicas":
		return "Replicas", ""
	case "spec.strategy.type":
		return "Strategy", ""
	case "spec.progressDeadlineSeconds":
		return "Progress deadline", ""
	case "spec.revisionHistoryLimit":
		return "Revision history limit", ""
	case "spec.template.spec.schedulerName":
		return "Scheduler", ""
	case "spec.template.spec.serviceAccountName":
		return "Service account", ""
	}

	if key, found := strings.CutPrefix(path, "metadata.annotations."); found {
		return "Annotation", key
	}
	if key, found := strings.CutPrefix(path, "metadata.labels."); found {
		return "Label", key
	}
	return "", ""
}

// resourceLabel names a compute request or limit the way a person says it.
func resourceLabel(trimmed string) string {
	kind := "limit"
	if strings.HasPrefix(trimmed, "resources.requests.") {
		kind = "request"
	}
	switch trimmed[strings.LastIndex(trimmed, ".")+1:] {
	case "cpu":
		return "CPU " + kind
	case "memory":
		return "Memory " + kind
	default:
		return "Resource " + kind
	}
}

// within pulls the value out of a keyed path segment: given `name=api]`, the
// `api`. It reads the first occurrence, which is the outermost, so a container
// is found before anything nested inside it.
func within(path, opener string) string {
	_, after, found := strings.Cut(path, opener)
	if !found {
		return ""
	}
	value, _, found := strings.Cut(after, "]")
	if !found {
		return ""
	}
	return value
}
