package domain

import "time"

// Health is the traffic light a row shows. It is derived from a resource's
// own status, never from HTTP success.
type Health string

// Health values.
const (
	HealthUnknown  Health = "unknown"
	HealthHealthy  Health = "healthy"
	HealthWarning  Health = "warning"
	HealthCritical Health = "critical"
	HealthProgress Health = "progressing"
)

// Kind identifies a Kubernetes resource type in the navigation tree.
//
// The catalogue is data rather than a switch statement, so adding a built-in
// kind — and later a custom resource — does not mean touching the list, detail
// and YAML paths separately.
type Kind string

// Built-in kinds covered by the MVP navigation.
const (
	KindPod                     Kind = "pods"
	KindDeployment              Kind = "deployments"
	KindStatefulSet             Kind = "statefulsets"
	KindDaemonSet               Kind = "daemonsets"
	KindJob                     Kind = "jobs"
	KindCronJob                 Kind = "cronjobs"
	KindReplicaSet              Kind = "replicasets"
	KindReplicationController   Kind = "replicationcontrollers"
	KindConfigMap               Kind = "configmaps"
	KindSecret                  Kind = "secrets"
	KindResourceQuota           Kind = "resourcequotas"
	KindLimitRange              Kind = "limitranges"
	KindHorizontalPodAutoscaler Kind = "horizontalpodautoscalers"
	KindPodDisruptionBudget     Kind = "poddisruptionbudgets"
	KindPriorityClass           Kind = "priorityclasses"
	KindRuntimeClass            Kind = "runtimeclasses"
	KindLease                   Kind = "leases"
	KindMutatingWebhook         Kind = "mutatingwebhookconfigurations"
	KindValidatingWebhook       Kind = "validatingwebhookconfigurations"
	KindService                 Kind = "services"
	KindEndpointSlice           Kind = "endpointslices"
	KindEndpoints               Kind = "endpoints"
	KindIngress                 Kind = "ingresses"
	KindIngressClass            Kind = "ingressclasses"
	KindNetworkPolicy           Kind = "networkpolicies"
	KindPersistentVolume        Kind = "persistentvolumes"
	KindPersistentVolumeClaim   Kind = "persistentvolumeclaims"
	KindStorageClass            Kind = "storageclasses"
	KindServiceAccount          Kind = "serviceaccounts"
	KindRole                    Kind = "roles"
	KindRoleBinding             Kind = "rolebindings"
	KindClusterRole             Kind = "clusterroles"
	KindClusterRoleBinding      Kind = "clusterrolebindings"
	KindNamespace               Kind = "namespaces"
	KindNode                    Kind = "nodes"
	KindEvent                   Kind = "events"

	KindCustomResourceDefinition Kind = "customresourcedefinitions"
)

// CustomKind names a custom resource type.
//
// Plural and group together are what makes it unique — two operators may both
// define "policies" — and the result cannot collide with a built-in kind,
// which is a bare plural with no dot in it.
func CustomKind(plural, group string) Kind { return Kind(plural + "." + group) }

// Category groups kinds in the sidebar.
type Category string

// Sidebar groups.
const (
	CategoryWorkloads Category = "Workloads"
	CategoryConfig    Category = "Config"
	CategoryNetwork   Category = "Network"
	CategoryStorage   Category = "Storage"
	CategoryAccess    Category = "Access Control"
	CategoryCluster   Category = "Cluster"

	// CategoryCustom holds what the cluster's own operators installed. Its
	// entries are discovered per cluster instead of being compiled in, so it
	// is grouped a second time by API group in the sidebar — a cluster with
	// twenty definitions across five groups is a list nobody can scan flat.
	CategoryCustom Category = "Custom Resources"
)

// KindInfo describes one entry in the resource catalogue.
type KindInfo struct {
	Kind     Kind     `json:"kind"`
	Title    string   `json:"title"`
	Category Category `json:"category"`

	Group    string `json:"group"`
	Version  string `json:"version"`
	Resource string `json:"resource"`

	Namespaced bool `json:"namespaced"`

	// Columns are the kind-specific column keys, in display order. Name,
	// namespace and age are implicit and rendered by the table itself.
	Columns []Column `json:"columns"`

	// Sensitive marks kinds whose values must stay masked until the user asks
	// for them explicitly.
	Sensitive bool `json:"sensitive"`

	// Standalone sits outside a collapsible group, the way Nodes, Namespaces
	// and Events do in the sidebar.
	Standalone bool `json:"standalone,omitempty"`

	// Custom marks an entry that came from a CustomResourceDefinition in the
	// cluster rather than from the compiled-in catalogue. Its columns are the
	// definition's own, so two clusters can show the same kind differently.
	Custom bool `json:"custom,omitempty"`

	// Actions are the changes this kind accepts beyond editing its manifest.
	//
	// They are declared here so the menu the user right-clicks and the code
	// that carries the request out read the same list and cannot come to
	// disagree about what is on offer. Custom resources have none: what an
	// operator's resource means is not something this application can know.
	Actions []ResourceAction `json:"actions,omitempty"`
}

// Column describes one kind-specific table column.
type Column struct {
	Key   string `json:"key"`
	Title string `json:"title"`

	// Mono renders the value in the monospace face, for numbers and IPs.
	Mono bool `json:"mono,omitempty"`

	// Path is the JSONPath a custom resource's value is read from, copied from
	// the definition's own printer columns. Built-in kinds leave it empty and
	// are rendered by compiled-in code instead.
	//
	// It stays on this side of the binding: the frontend receives the value
	// already resolved in ResourceRow.Fields and has no expression to evaluate.
	Path string `json:"-"`
}

// ResourceRef addresses one object.
type ResourceRef struct {
	Kind      Kind   `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

// ResourceRow is one line of a resource table.
//
// Kind-specific values live in Fields rather than in a per-kind struct, so a
// single virtualized table renders every kind — including custom resources,
// whose columns are not known at compile time.
type ResourceRow struct {
	// Key is the object's identity within its kind: "namespace/name", or the
	// bare name for a cluster-scoped kind. Rows are ordered and patched by it
	// rather than by UID, because a UID changes when an object is recreated
	// under the same name and the table would lose the row the user is on.
	Key string `json:"key"`

	UID       string `json:"uid"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`

	Health Health `json:"health"`
	Status string `json:"status,omitempty"`

	CreatedAt time.Time `json:"createdAt"`

	Fields map[string]string `json:"fields,omitempty"`
}

// Sort keys the table understands beyond a kind's own column keys. Anything
// else is read from a row's Fields, so a custom resource sorts by its own
// printer columns without a compiled-in case for it.
const (
	SortKeyCreated   = "createdAt"
	SortKeyName      = "name"
	SortKeyNamespace = "namespace"
	SortKeyStatus    = "status"
)

// ListQuery is what a table asks for: which slice of which order.
//
// Filtering, sorting and windowing all happen where the whole truth is, which
// is here rather than in the renderer. A filter applied to a window would only
// ever search what happened to be sent, and would report a resource that
// exists as missing.
type ListQuery struct {
	Namespace string `json:"namespace"`

	// Filter matches a name fragment, case-insensitively.
	Filter string `json:"filter,omitempty"`

	// SortKey is empty for the default order, which is newest first: an
	// engineer opening a list is almost always looking for what just changed.
	SortKey  string `json:"sortKey,omitempty"`
	SortDesc bool   `json:"sortDesc,omitempty"`

	Offset int `json:"offset,omitempty"`
	Limit  int `json:"limit,omitempty"`

	// Token identifies the table the frontend is building, and comes back on
	// every patch computed for it. A patch is described in terms of the window
	// its query produced, so one still in flight when the filter changes would
	// otherwise be applied to a table it does not describe.
	Token string `json:"token,omitempty"`
}

// Window bounds for one table request. The frontend renders a window of rows
// and appends as the user scrolls, so a page is large enough to scroll through
// without a round trip and small enough to cross the binding in one piece.
const (
	DefaultListLimit = 500
	MaxListLimit     = 2000
)

// Normalise fills in the defaults and clamps the window, so every caller gets
// the same order and no caller can ask for a page too large to send.
func (q ListQuery) Normalise() ListQuery {
	if q.SortKey == "" {
		q.SortKey = SortKeyCreated
		q.SortDesc = true
	}
	if q.Limit <= 0 {
		q.Limit = DefaultListLimit
	}
	if q.Limit > MaxListLimit {
		q.Limit = MaxListLimit
	}
	if q.Offset < 0 {
		q.Offset = 0
	}
	return q
}

// ResourcePage is a rendered table: the rows plus the columns they fill.
type ResourcePage struct {
	Kind    Kind          `json:"kind"`
	Columns []Column      `json:"columns"`
	Rows    []ResourceRow `json:"rows"`

	// Namespaced tells the UI whether to show the namespace column.
	Namespaced bool `json:"namespaced"`

	// Total is how many objects of this kind the view holds before the filter,
	// Matched how many the filter left, and Rows the window starting at
	// Offset. Reporting all three is what lets the table say "40 of 12000"
	// instead of quietly showing a prefix and calling it the list.
	Total   int `json:"total"`
	Matched int `json:"matched"`
	Offset  int `json:"offset"`

	// Loading marks a page served from a first API request while the watch is
	// still filling its cache. The counts are a floor, not the truth, and the
	// watch will correct them within moments.
	Loading bool `json:"loading,omitempty"`
}

// ContainerInfo describes one container of a pod, for the container selector
// used by logs and the terminal.
type ContainerInfo struct {
	Name  string `json:"name"`
	Image string `json:"image"`

	Ready        bool   `json:"ready"`
	State        string `json:"state"`
	RestartCount int32  `json:"restartCount"`

	// Init marks init containers, whose logs are read differently.
	Init bool `json:"init"`
}

// PodDetail is the overview tab of a pod.
type PodDetail struct {
	Ref ResourceRef `json:"ref"`

	Status   string `json:"status"`
	Health   Health `json:"health"`
	Node     string `json:"node"`
	PodIP    string `json:"podIp"`
	HostIP   string `json:"hostIp"`
	QOSClass string `json:"qosClass"`

	StartedAt *time.Time `json:"startedAt,omitempty"`

	Containers     []ContainerInfo `json:"containers"`
	InitContainers []ContainerInfo `json:"initContainers"`

	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`

	Volumes    []string    `json:"volumes,omitempty"`
	Conditions []Condition `json:"conditions,omitempty"`

	// Ports are the container ports, offered as port-forward suggestions.
	Ports []ContainerPort `json:"ports,omitempty"`
}

// Condition is a status condition, shown on detail pages.
type Condition struct {
	Type    string     `json:"type"`
	Status  string     `json:"status"`
	Reason  string     `json:"reason,omitempty"`
	Message string     `json:"message,omitempty"`
	Since   *time.Time `json:"since,omitempty"`
}

// ContainerPort is a port a container declares.
type ContainerPort struct {
	Name     string `json:"name,omitempty"`
	Port     int32  `json:"port"`
	Protocol string `json:"protocol"`
}

// EventRow is one line of the event viewer.
type EventRow struct {
	UID string `json:"uid"`

	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Object  string `json:"object"`
	Message string `json:"message"`

	Namespace string `json:"namespace,omitempty"`
	Count     int32  `json:"count"`

	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// ClusterOverview is the cluster dashboard.
//
// Every count is optional in practice: a cluster where the engineer may only
// read one namespace still renders, with the parts it cannot see left unset.
type ClusterOverview struct {
	ClusterID string `json:"clusterId"`

	ServerVersion string `json:"serverVersion,omitempty"`
	Platform      string `json:"platform,omitempty"`

	Nodes      Counter `json:"nodes"`
	Pods       Counter `json:"pods"`
	Namespaces int     `json:"namespaces"`

	Deployments  int `json:"deployments"`
	StatefulSets int `json:"statefulSets"`
	DaemonSets   int `json:"daemonSets"`

	// Metrics are absent when the cluster has no metrics-server. That is a
	// normal state, not an error.
	Metrics *ClusterMetrics `json:"metrics,omitempty"`

	RecentWarnings []EventRow `json:"recentWarnings,omitempty"`
}

// Counter is a ready/total pair.
type Counter struct {
	Ready int `json:"ready"`
	Total int `json:"total"`
}

// ClusterMetrics is aggregate usage, present only when metrics-server answers.
type ClusterMetrics struct {
	CPUUsedMilli     int64 `json:"cpuUsedMilli"`
	CPUCapacityMilli int64 `json:"cpuCapacityMilli"`

	MemoryUsedBytes     int64 `json:"memoryUsedBytes"`
	MemoryCapacityBytes int64 `json:"memoryCapacityBytes"`
}

// SearchHit is one result of the global resource search.
type SearchHit struct {
	Kind      Kind   `json:"kind"`
	KindTitle string `json:"kindTitle"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Health    Health `json:"health"`
}

// KindPresence is how many objects of a kind exist in the current namespace
// (or cluster-wide for cluster-scoped kinds). Zero means the sidebar should
// fade that entry.
type KindPresence struct {
	Kind  Kind `json:"kind"`
	Count int  `json:"count"`
}

// DataEntry is one key from a ConfigMap or Secret `data` (or `binaryData`) map.
//
// Value is exactly what Kubernetes stores on the object: plaintext for
// ConfigMap `data`, and base64 for Secret `data` and ConfigMap `binaryData`.
// It is never decoded — revealing a secret in the UI must show the stored
// encoding, not the plaintext behind it.
type DataEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`

	// Binary marks ConfigMap binaryData (and is unused for Secrets, whose
	// data is always base64 in the API JSON).
	Binary bool `json:"binary,omitempty"`
}

// ResourceInspect is the right-hand inspector for one object.
type ResourceInspect struct {
	Ref       ResourceRef `json:"ref"`
	CreatedAt time.Time   `json:"createdAt"`

	// Type is a Secret's type (Opaque, kubernetes.io/tls, …).
	Type string `json:"type,omitempty"`

	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`

	// Properties are kind-specific inspector rows (selector, replica counts, …).
	Properties []InspectProperty `json:"properties,omitempty"`

	Data []DataEntry `json:"data,omitempty"`
}

// InspectProperty is one extra row in the right-hand inspector.
type InspectProperty struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Mono  bool   `json:"mono,omitempty"`
}
