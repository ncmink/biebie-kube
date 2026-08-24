// Package domain holds the vocabulary of Biebie Kube: clusters, their
// connection lifecycle, and the customer context they belong to.
//
// Nothing here talks to Kubernetes, to disk, or to Biebie Access. Keeping the
// vocabulary free of infrastructure is what lets the services above it be
// tested without a cluster.
package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	bctx "github.com/ncmink/biebie-protocol/context"
)

// Cluster is a Kubernetes endpoint the engineer works with, grouped under the
// customer and environment it belongs to.
//
// Identity is an internal UUID. A cluster name is a label people change, and
// two customers routinely name a cluster "production"; keying anything off the
// name would silently merge two customers' state.
type Cluster struct {
	ID string `json:"id"`

	Name string `json:"name"`

	CustomerID   string `json:"customerId"`
	CustomerName string `json:"customerName"`

	EnvironmentID   string           `json:"environmentId"`
	EnvironmentName string           `json:"environmentName"`
	EnvironmentKind bctx.Environment `json:"environmentKind"`

	// Server is the API endpoint, shown in the UI and used for the
	// reachability probe when a connection fails.
	Server string `json:"server"`

	// KubeconfigRef points at an indexed kubeconfig file. The file's contents
	// are never copied into this record and never leave the Go process.
	KubeconfigRef string `json:"kubeconfigRef"`
	ContextName   string `json:"contextName"`

	Access AccessRequirement `json:"access"`

	// Archived keeps a cluster out of the list without touching the customer it
	// belongs to, so taking it back out returns it to that customer's section
	// rather than to wherever it was last filed.
	Archived bool `json:"archived"`

	Labels map[string]string `json:"labels,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AccessRequirement records whether reaching this cluster depends on Biebie
// Access bringing a customer network up first.
//
// A local Docker Desktop cluster needs nothing. A customer's production RKE2
// behind Forcepoint needs a named profile. Biebie Kube must work in both cases,
// so this is a property of the cluster rather than an assumption of the app.
type AccessRequirement struct {
	Required  bool   `json:"required"`
	ProfileID string `json:"profileId,omitempty"`
}

// Title renders the customer → environment → cluster trail.
func (c Cluster) Title() string {
	parts := make([]string, 0, 3)
	for _, part := range []string{c.CustomerName, c.EnvironmentName, c.Name} {
		if strings.TrimSpace(part) != "" {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, " / ")
}

// IsProduction reports whether operations here deserve extra confirmation.
func (c Cluster) IsProduction() bool { return c.EnvironmentKind.IsProduction() }

// CustomerKey identifies the group this cluster is listed under.
//
// The identifier is preferred over the display name for the same reason cluster
// identity is a UUID: a name is a label people rewrite, and a customer the
// engineer hid from the list must stay hidden across that rename. A cluster
// with neither belongs to the empty group, because a kubeconfig says nothing
// about who owns a cluster.
func (c Cluster) CustomerKey() string {
	if id := strings.TrimSpace(c.CustomerID); id != "" {
		return id
	}
	return strings.TrimSpace(c.CustomerName)
}

// CustomerLabel is the heading the group is shown under.
func (c Cluster) CustomerLabel() string {
	if name := strings.TrimSpace(c.CustomerName); name != "" {
		return name
	}
	if id := strings.TrimSpace(c.CustomerID); id != "" {
		return id
	}
	return UngroupedLabel
}

// GroupKey is the section of the cluster list this cluster appears in: the
// archive once it has been put away, otherwise its customer.
func (c Cluster) GroupKey() string {
	if c.Archived {
		return ArchivedKey
	}
	return c.CustomerKey()
}

// GroupLabel is the heading of that section.
func (c Cluster) GroupLabel() string {
	if c.Archived {
		return ArchivedLabel
	}
	return c.CustomerLabel()
}

// Host returns the API server host without scheme or port, for reachability
// probes and for compact display.
func (c Cluster) Host() string {
	server := strings.TrimSpace(c.Server)
	if server == "" {
		return ""
	}
	if !strings.Contains(server, "://") {
		server = "https://" + server
	}
	u, err := url.Parse(server)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// HostPort returns host:port for the API server, defaulting to the HTTPS port
// Kubernetes uses when the endpoint omits one.
func (c Cluster) HostPort() string {
	server := strings.TrimSpace(c.Server)
	if server == "" {
		return ""
	}
	if !strings.Contains(server, "://") {
		server = "https://" + server
	}
	u, err := url.Parse(server)
	if err != nil {
		return ""
	}
	port := u.Port()
	if port == "" {
		port = "443"
	}
	return u.Hostname() + ":" + port
}

// ClusterInput is what the UI submits when adding or editing a cluster.
type ClusterInput struct {
	Name string `json:"name"`

	CustomerID   string `json:"customerId"`
	CustomerName string `json:"customerName"`

	EnvironmentID   string           `json:"environmentId"`
	EnvironmentName string           `json:"environmentName"`
	EnvironmentKind bctx.Environment `json:"environmentKind"`

	KubeconfigRef string `json:"kubeconfigRef"`
	ContextName   string `json:"contextName"`

	RequiresAccess  bool   `json:"requiresAccess"`
	AccessProfileID string `json:"accessProfileId"`

	Labels map[string]string `json:"labels,omitempty"`
}

// Validate rejects an input the rest of the application could not act on.
func (in ClusterInput) Validate() error {
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("cluster name is required")
	}
	if strings.TrimSpace(in.KubeconfigRef) == "" {
		return errors.New("a kubeconfig must be selected")
	}
	if strings.TrimSpace(in.ContextName) == "" {
		return errors.New("a kubeconfig context must be selected")
	}
	if in.RequiresAccess && strings.TrimSpace(in.AccessProfileID) == "" {
		return errors.New("a Biebie Access profile is required when this cluster needs customer network access")
	}
	switch in.EnvironmentKind {
	case bctx.EnvironmentUnknown, bctx.EnvironmentDevelopment,
		bctx.EnvironmentStaging, bctx.EnvironmentProduction:
	default:
		return fmt.Errorf("unknown environment kind %q", in.EnvironmentKind)
	}
	return nil
}

// ClusterPreference remembers per-cluster UI choices, so switching customers
// does not reset where the engineer was working.
type ClusterPreference struct {
	ClusterID     string `json:"clusterId"`
	LastNamespace string `json:"lastNamespace"`
}

// AllNamespaces is the sentinel namespace meaning "do not filter".
const AllNamespaces = ""
