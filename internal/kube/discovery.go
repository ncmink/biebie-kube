package kube

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

// GVRFor maps a catalogue entry to the group/version/resource the dynamic
// client addresses.
func GVRFor(group, version, resource string) schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
}

// Namespaces lists namespace names, sorted for a stable selector.
//
// Listing namespaces is often the first call denied by RBAC on a
// tightly-scoped account. That is not a connection failure: the caller falls
// back to the namespace the kubeconfig context names.
func (c *ClusterClient) Namespaces(ctx context.Context) ([]string, error) {
	list, err := c.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	out := make([]string, 0, len(list.Items))
	for _, ns := range list.Items {
		out = append(out, ns.Name)
	}
	sort.Strings(out)
	return out, nil
}

// APIResource describes a resource type the cluster serves, used to hide
// navigation entries a cluster does not have and, later, to list custom
// resources without compiled-in Go types.
type APIResource struct {
	Group      string   `json:"group"`
	Version    string   `json:"version"`
	Resource   string   `json:"resource"`
	Kind       string   `json:"kind"`
	Namespaced bool     `json:"namespaced"`
	Verbs      []string `json:"verbs,omitempty"`
}

// DiscoveryIssue reports one partial-discovery failure.
type DiscoveryIssue struct {
	Group   string `json:"group,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// DiscoveryResult is the merged outcome of API discovery for one cluster.
type DiscoveryResult struct {
	Resources []APIResource    `json:"resources"`
	Issues    []DiscoveryIssue `json:"issues"`
	Complete  bool             `json:"complete"`
}

// ServerResources lists every resource type the API server serves.
//
// Deprecated: use DiscoverResources, which merges versions and reports partial
// failures. ServerResources remains for callers that only need a flat list.
func (c *ClusterClient) ServerResources(ctx context.Context) ([]APIResource, error) {
	result, err := c.DiscoverResources(ctx)
	if err != nil && len(result.Resources) == 0 {
		return nil, err
	}
	return result.Resources, nil
}

// DiscoverResources lists listable resource types, merging multiple served
// versions to one entry per group/resource using the API group's preferred
// version when available.
func (c *ClusterClient) DiscoverResources(ctx context.Context) (DiscoveryResult, error) {
	type payload struct {
		groups []*metav1.APIGroup
		lists  []*metav1.APIResourceList
		err    error
	}
	done := make(chan payload, 1)
	go func() {
		groups, lists, err := c.Discovery.ServerGroupsAndResources()
		done <- payload{groups: groups, lists: lists, err: err}
	}()

	var (
		groups []*metav1.APIGroup
		lists  []*metav1.APIResourceList
		err    error
	)
	select {
	case <-ctx.Done():
		return DiscoveryResult{}, ctx.Err()
	case r := <-done:
		groups, lists, err = r.groups, r.lists, r.err
	}

	result := DiscoveryResult{Complete: err == nil}
	if err != nil {
		var partial *discovery.ErrGroupDiscoveryFailed
		if errors.As(err, &partial) {
			for group, groupErr := range partial.Groups {
				result.Issues = append(result.Issues, DiscoveryIssue{
					Group:   group.Group,
					Code:    issueCode(groupErr),
					Message: sanitizeIssue(groupErr),
				})
			}
			sort.Slice(result.Issues, func(i, j int) bool {
				return result.Issues[i].Group < result.Issues[j].Group
			})
		} else if len(lists) == 0 {
			return DiscoveryResult{}, fmt.Errorf("discover API resources: %w", err)
		} else {
			result.Issues = append(result.Issues, DiscoveryIssue{
				Code:    "discovery_error",
				Message: sanitizeIssue(err),
			})
		}
	}

	preferred := preferredVersions(groups)
	result.Resources = mergeResourceLists(lists, preferred)
	return result, nil
}

func preferredVersions(groups []*metav1.APIGroup) map[string]string {
	out := make(map[string]string, len(groups))
	for _, group := range groups {
		if group == nil {
			continue
		}
		if group.PreferredVersion.Version != "" {
			out[group.Name] = group.PreferredVersion.Version
			continue
		}
		for _, version := range group.Versions {
			if version.Version != "" {
				out[group.Name] = version.Version
				break
			}
		}
	}
	return out
}

type resourceCandidate struct {
	group      string
	resource   string
	version    string
	kind       string
	namespaced bool
	verbs      []string
}

func mergeResourceLists(lists []*metav1.APIResourceList, preferred map[string]string) []APIResource {
	byKey := make(map[string]resourceCandidate)
	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, resource := range list.APIResources {
			if strings.Contains(resource.Name, "/") {
				continue
			}
			if !listableVerbs(resource.Verbs) {
				continue
			}
			key := gv.Group + "/" + resource.Name
			candidate := resourceCandidate{
				group:      gv.Group,
				resource:   resource.Name,
				version:    gv.Version,
				kind:       resource.Kind,
				namespaced: resource.Namespaced,
				verbs:      append([]string(nil), resource.Verbs...),
			}
			existing, ok := byKey[key]
			if !ok {
				byKey[key] = candidate
				continue
			}
			want := preferred[gv.Group]
			if versionRank(candidate.version, want) < versionRank(existing.version, want) {
				byKey[key] = candidate
			}
		}
	}

	out := make([]APIResource, 0, len(byKey))
	for _, candidate := range byKey {
		out = append(out, APIResource{
			Group:      candidate.group,
			Version:    candidate.version,
			Resource:   candidate.resource,
			Kind:       candidate.kind,
			Namespaced: candidate.namespaced,
			Verbs:      candidate.verbs,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Group != out[j].Group {
			return out[i].Group < out[j].Group
		}
		return out[i].Resource < out[j].Resource
	})
	return out
}

func listableVerbs(verbs []string) bool {
	for _, verb := range verbs {
		if verb == "list" {
			return true
		}
	}
	return false
}

func versionRank(version, preferred string) int {
	if preferred != "" && version == preferred {
		return 0
	}
	return 1
}

func issueCode(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "forbidden"):
		return "forbidden"
	case strings.Contains(msg, "unauthorized"):
		return "unauthorized"
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return "timeout"
	default:
		return "unavailable"
	}
}

func sanitizeIssue(err error) string {
	if err == nil {
		return ""
	}
	return strings.TrimSpace(err.Error())
}

// Supports reports whether the cluster serves a resource type.
func Supports(resources []APIResource, gvr schema.GroupVersionResource) bool {
	for _, resource := range resources {
		if resource.Group == gvr.Group && resource.Resource == gvr.Resource {
			return true
		}
	}
	return false
}
