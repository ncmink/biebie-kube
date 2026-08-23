package domain

// catalogue is the resource navigation tree, in sidebar order.
//
// Group/Version/Resource are recorded here so every service reaches Kubernetes
// through the dynamic client with the same mapping, and adding a kind is one
// entry rather than a new code path.
var catalogue = []KindInfo{
	{
		Kind: KindNode, Title: "Nodes", Category: CategoryCluster,
		Version: "v1", Resource: "nodes", Namespaced: false, Standalone: true,
		Columns: []Column{
			{Key: "status", Title: "Status"},
			{Key: "roles", Title: "Roles"},
			{Key: "version", Title: "Version", Mono: true},
			{Key: "cpu", Title: "CPU", Mono: true},
			{Key: "memory", Title: "Memory", Mono: true},
		},
	},
	{
		Kind: KindPod, Title: "Pods", Category: CategoryWorkloads,
		Version: "v1", Resource: "pods", Namespaced: true,
		Columns: []Column{
			{Key: "ready", Title: "Ready", Mono: true},
			{Key: "status", Title: "Status"},
			{Key: "restarts", Title: "Restarts", Mono: true},
			{Key: "cpu", Title: "CPU", Mono: true},
			{Key: "memory", Title: "Memory", Mono: true},
			{Key: "node", Title: "Node"},
		},
	},
	{
		Kind: KindDeployment, Title: "Deployments", Category: CategoryWorkloads,
		Group: "apps", Version: "v1", Resource: "deployments", Namespaced: true,
		Columns: []Column{
			{Key: "ready", Title: "Ready", Mono: true},
			{Key: "upToDate", Title: "Up to date", Mono: true},
			{Key: "available", Title: "Available", Mono: true},
		},
	},
	{
		Kind: KindStatefulSet, Title: "Stateful Sets", Category: CategoryWorkloads,
		Group: "apps", Version: "v1", Resource: "statefulsets", Namespaced: true,
		Columns: []Column{
			{Key: "ready", Title: "Ready", Mono: true},
			{Key: "image", Title: "Image"},
		},
	},
	{
		Kind: KindDaemonSet, Title: "Daemon Sets", Category: CategoryWorkloads,
		Group: "apps", Version: "v1", Resource: "daemonsets", Namespaced: true,
		Columns: []Column{
			{Key: "ready", Title: "Ready", Mono: true},
			{Key: "desired", Title: "Desired", Mono: true},
			{Key: "available", Title: "Available", Mono: true},
		},
	},
	{
		Kind: KindReplicaSet, Title: "Replica Sets", Category: CategoryWorkloads,
		Group: "apps", Version: "v1", Resource: "replicasets", Namespaced: true,
		Columns: []Column{
			{Key: "ready", Title: "Ready", Mono: true},
			{Key: "desired", Title: "Desired", Mono: true},
		},
	},
	{
		Kind: KindReplicationController, Title: "Replication Controllers", Category: CategoryWorkloads,
		Version: "v1", Resource: "replicationcontrollers", Namespaced: true,
		Columns: []Column{
			{Key: "ready", Title: "Ready", Mono: true},
			{Key: "desired", Title: "Desired", Mono: true},
		},
	},
	{
		Kind: KindJob, Title: "Jobs", Category: CategoryWorkloads,
		Group: "batch", Version: "v1", Resource: "jobs", Namespaced: true,
		Columns: []Column{
			{Key: "completions", Title: "Completions", Mono: true},
			{Key: "duration", Title: "Duration", Mono: true},
		},
	},
	{
		Kind: KindCronJob, Title: "Cron Jobs", Category: CategoryWorkloads,
		Group: "batch", Version: "v1", Resource: "cronjobs", Namespaced: true,
		Columns: []Column{
			{Key: "schedule", Title: "Schedule", Mono: true},
			{Key: "suspend", Title: "Suspended"},
			{Key: "active", Title: "Active", Mono: true},
			{Key: "lastSchedule", Title: "Last run"},
		},
	},

	{
		Kind: KindConfigMap, Title: "Config Maps", Category: CategoryConfig,
		Version: "v1", Resource: "configmaps", Namespaced: true,
		Columns: []Column{{Key: "keys", Title: "Keys", Mono: true}},
	},
	{
		Kind: KindSecret, Title: "Secrets", Category: CategoryConfig,
		Version: "v1", Resource: "secrets", Namespaced: true, Sensitive: true,
		Columns: []Column{
			{Key: "type", Title: "Type"},
			{Key: "keys", Title: "Keys", Mono: true},
		},
	},
	{
		Kind: KindResourceQuota, Title: "Resource Quotas", Category: CategoryConfig,
		Version: "v1", Resource: "resourcequotas", Namespaced: true,
	},
	{
		Kind: KindLimitRange, Title: "Limit Ranges", Category: CategoryConfig,
		Version: "v1", Resource: "limitranges", Namespaced: true,
	},
	{
		Kind: KindHorizontalPodAutoscaler, Title: "Horizontal Pod Autoscalers", Category: CategoryConfig,
		Group: "autoscaling", Version: "v2", Resource: "horizontalpodautoscalers", Namespaced: true,
	},
	{
		Kind: KindPodDisruptionBudget, Title: "Pod Disruption Budgets", Category: CategoryConfig,
		Group: "policy", Version: "v1", Resource: "poddisruptionbudgets", Namespaced: true,
		Columns: []Column{
			{Key: "minAvailable", Title: "Min available", Mono: true},
			{Key: "healthy", Title: "Healthy", Mono: true},
		},
	},
	{
		Kind: KindPriorityClass, Title: "Priority Classes", Category: CategoryConfig,
		Group: "scheduling.k8s.io", Version: "v1", Resource: "priorityclasses", Namespaced: false,
	},
	{
		Kind: KindRuntimeClass, Title: "Runtime Classes", Category: CategoryConfig,
		Group: "node.k8s.io", Version: "v1", Resource: "runtimeclasses", Namespaced: false,
	},
	{
		Kind: KindLease, Title: "Leases", Category: CategoryConfig,
		Group: "coordination.k8s.io", Version: "v1", Resource: "leases", Namespaced: true,
	},
	{
		Kind: KindMutatingWebhook, Title: "Mutating Webhook Configurations", Category: CategoryConfig,
		Group: "admissionregistration.k8s.io", Version: "v1", Resource: "mutatingwebhookconfigurations", Namespaced: false,
	},
	{
		Kind: KindValidatingWebhook, Title: "Validating Webhook Configurations", Category: CategoryConfig,
		Group: "admissionregistration.k8s.io", Version: "v1", Resource: "validatingwebhookconfigurations", Namespaced: false,
	},

	{
		Kind: KindService, Title: "Services", Category: CategoryNetwork,
		Version: "v1", Resource: "services", Namespaced: true,
		Columns: []Column{
			{Key: "type", Title: "Type"},
			{Key: "clusterIp", Title: "Cluster IP", Mono: true},
			{Key: "ports", Title: "Ports", Mono: true},
		},
	},
	{
		Kind: KindEndpointSlice, Title: "Endpoint Slices", Category: CategoryNetwork,
		Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices", Namespaced: true,
	},
	{
		Kind: KindEndpoints, Title: "Endpoints", Category: CategoryNetwork,
		Version: "v1", Resource: "endpoints", Namespaced: true,
	},
	{
		Kind: KindIngress, Title: "Ingresses", Category: CategoryNetwork,
		Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Namespaced: true,
		Columns: []Column{
			{Key: "class", Title: "Class"},
			{Key: "hosts", Title: "Hosts"},
		},
	},
	{
		Kind: KindIngressClass, Title: "Ingress Classes", Category: CategoryNetwork,
		Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses", Namespaced: false,
	},
	{
		Kind: KindNetworkPolicy, Title: "Network Policies", Category: CategoryNetwork,
		Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies", Namespaced: true,
		Columns: []Column{{Key: "podSelector", Title: "Pod selector"}},
	},

	{
		Kind: KindPersistentVolumeClaim, Title: "Persistent Volume Claims", Category: CategoryStorage,
		Version: "v1", Resource: "persistentvolumeclaims", Namespaced: true,
		Columns: []Column{
			{Key: "status", Title: "Status"},
			{Key: "capacity", Title: "Capacity", Mono: true},
			{Key: "storageClass", Title: "Storage class"},
		},
	},
	{
		Kind: KindPersistentVolume, Title: "Persistent Volumes", Category: CategoryStorage,
		Version: "v1", Resource: "persistentvolumes", Namespaced: false,
		Columns: []Column{
			{Key: "status", Title: "Status"},
			{Key: "capacity", Title: "Capacity", Mono: true},
			{Key: "claim", Title: "Claim"},
		},
	},
	{
		Kind: KindStorageClass, Title: "Storage Classes", Category: CategoryStorage,
		Group: "storage.k8s.io", Version: "v1", Resource: "storageclasses", Namespaced: false,
		Columns: []Column{{Key: "provisioner", Title: "Provisioner"}},
	},

	{
		Kind: KindNamespace, Title: "Namespaces", Category: CategoryCluster,
		Version: "v1", Resource: "namespaces", Namespaced: false, Standalone: true,
		Columns: []Column{{Key: "status", Title: "Status"}},
	},
	{
		Kind: KindEvent, Title: "Events", Category: CategoryCluster,
		Version: "v1", Resource: "events", Namespaced: true, Standalone: true,
		Columns: []Column{
			{Key: "type", Title: "Type"},
			{Key: "reason", Title: "Reason"},
			{Key: "object", Title: "Object"},
			{Key: "message", Title: "Message"},
			{Key: "count", Title: "Count", Mono: true},
		},
	},

	{
		Kind: KindServiceAccount, Title: "Service Accounts", Category: CategoryAccess,
		Version: "v1", Resource: "serviceaccounts", Namespaced: true,
	},
	{
		Kind: KindClusterRole, Title: "Cluster Roles", Category: CategoryAccess,
		Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterroles", Namespaced: false,
	},
	{
		Kind: KindRole, Title: "Roles", Category: CategoryAccess,
		Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "roles", Namespaced: true,
	},
	{
		Kind: KindClusterRoleBinding, Title: "Cluster Role Bindings", Category: CategoryAccess,
		Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "clusterrolebindings", Namespaced: false,
	},
	{
		Kind: KindRoleBinding, Title: "Role Bindings", Category: CategoryAccess,
		Group: "rbac.authorization.k8s.io", Version: "v1", Resource: "rolebindings", Namespaced: true,
	},

	// The definitions sit at the head of the custom section, before the kinds
	// they describe, because they are how an engineer finds out what an
	// unfamiliar cluster's operators installed in the first place.
	{
		Kind: KindCustomResourceDefinition, Title: "Definitions", Category: CategoryCustom,
		Group: "apiextensions.k8s.io", Version: "v1", Resource: "customresourcedefinitions", Namespaced: false,
		Columns: []Column{
			{Key: "group", Title: "Group"},
			{Key: "kind", Title: "Kind"},
			{Key: "scope", Title: "Scope"},
			{Key: "versions", Title: "Versions", Mono: true},
		},
	},
}

var byKind = func() map[Kind]KindInfo {
	m := make(map[Kind]KindInfo, len(catalogue))
	for _, info := range catalogue {
		m[info.Kind] = info
	}
	return m
}()

// Catalogue returns every navigable kind, in sidebar order.
func Catalogue() []KindInfo {
	out := make([]KindInfo, len(catalogue))
	copy(out, catalogue)
	return out
}

// Lookup finds a kind's metadata.
func Lookup(kind Kind) (KindInfo, bool) {
	info, ok := byKind[kind]
	return info, ok
}

// SearchableKinds are the kinds the global search scans. Searching every kind
// in a large cluster is slow and mostly noise; these are what engineers
// actually look for by name.
func SearchableKinds() []Kind {
	return []Kind{
		KindPod, KindDeployment, KindStatefulSet, KindDaemonSet,
		KindService, KindIngress, KindConfigMap, KindSecret,
		KindJob, KindCronJob, KindPersistentVolumeClaim,
	}
}
