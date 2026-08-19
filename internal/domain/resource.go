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
)

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
}

// Column describes one kind-specific table column.
type Column struct {
	Key   string `json:"key"`
	Title string `json:"title"`

	// Mono renders the value in the monospace face, for numbers and IPs.
	Mono bool `json:"mono,omitempty"`
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
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`

	Health Health `json:"health"`
	Status string `json:"status,omitempty"`

	CreatedAt time.Time `json:"createdAt"`

	Fields map[string]string `json:"fields,omitempty"`
}

// ResourcePage is a rendered table: the rows plus the columns they fill.
type ResourcePage struct {
	Kind    Kind          `json:"kind"`
	Columns []Column      `json:"columns"`
	Rows    []ResourceRow `json:"rows"`

	// Namespaced tells the UI whether to show the namespace column.
	Namespaced bool `json:"namespaced"`

	// Truncated is set when a very large cluster returned more objects than
	// the table will render at once.
	Truncated bool `json:"truncated,omitempty"`
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

	Data []DataEntry `json:"data,omitempty"`
}
