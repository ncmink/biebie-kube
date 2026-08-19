package kube

import (
	"context"
	"fmt"
	"sort"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
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
	Group      string `json:"group"`
	Version    string `json:"version"`
	Resource   string `json:"resource"`
	Kind       string `json:"kind"`
	Namespaced bool   `json:"namespaced"`
}

// ServerResources lists every resource type the API server serves.
//
// A partial-discovery error is expected in the wild: an aggregated API server
// that is down makes discovery incomplete, and refusing to show anything
// because one extension is unhealthy would be worse than showing the rest.
func (c *ClusterClient) ServerResources(ctx context.Context) ([]APIResource, error) {
	type result struct {
		lists []*metav1.APIResourceList
		err   error
	}
	done := make(chan result, 1)
	go func() {
		_, lists, err := c.Discovery.ServerGroupsAndResources()
		done <- result{lists: lists, err: err}
	}()

	var lists []*metav1.APIResourceList
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case r := <-done:
		if r.err != nil && len(r.lists) == 0 {
			return nil, fmt.Errorf("discover API resources: %w", r.err)
		}
		lists = r.lists
	}

	out := make([]APIResource, 0, 256)
	for _, list := range lists {
		if list == nil {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, resource := range list.APIResources {
			// Subresources such as pods/log are addressed through their parent
			// and must not appear as navigable types.
			if strings.Contains(resource.Name, "/") {
				continue
			}
			out = append(out, APIResource{
				Group:      gv.Group,
				Version:    gv.Version,
				Resource:   resource.Name,
				Kind:       resource.Kind,
				Namespaced: resource.Namespaced,
			})
		}
	}
	return out, nil
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
