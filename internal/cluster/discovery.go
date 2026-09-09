package cluster

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/kube"
)

type catalogueInput struct {
	clusterID    string
	sessionEpoch string
	discovery    kube.DiscoveryResult
	customs      []kube.CustomResource
	crdErr       error
	observedAt   time.Time
}

// CatalogueInput is the exported test and tooling surface for catalogue merges.
type CatalogueInput struct {
	ClusterID    string
	SessionEpoch string
	Discovery    kube.DiscoveryResult
	Customs      []kube.CustomResource
	CRDErr       error
}

// BuildCatalogue merges discovery inputs into navigation and a snapshot.
func BuildCatalogue(in CatalogueInput) ([]domain.KindInfo, domain.DiscoverySnapshot) {
	return buildCatalogue(catalogueInput{
		clusterID:    in.ClusterID,
		sessionEpoch: in.SessionEpoch,
		discovery:    in.Discovery,
		customs:      in.Customs,
		crdErr:       in.CRDErr,
	})
}

// buildCatalogue merges built-in kinds, API discovery and CRD metadata.
func buildCatalogue(in catalogueInput) ([]domain.KindInfo, domain.DiscoverySnapshot) {
	if in.observedAt.IsZero() {
		in.observedAt = time.Now().UTC()
	}

	snapshot := domain.DiscoverySnapshot{
		ClusterID:    in.clusterID,
		SessionEpoch: in.sessionEpoch,
		ObservedAt:   in.observedAt,
		Complete:     in.discovery.Complete && len(in.discovery.Resources) > 0,
	}
	for _, issue := range in.discovery.Issues {
		snapshot.Issues = append(snapshot.Issues, domain.DiscoveryIssue{
			Group:   issue.Group,
			Code:    issue.Code,
			Message: issue.Message,
		})
	}
	if in.crdErr != nil {
		snapshot.Issues = append(snapshot.Issues, domain.DiscoveryIssue{
			Code:    crdIssueCode(in.crdErr),
			Message: strings.TrimSpace(in.crdErr.Error()),
		})
	}

	discovered := make(map[string]kube.APIResource, len(in.discovery.Resources))
	for _, resource := range in.discovery.Resources {
		discovered[resource.Group+"/"+resource.Resource] = resource
		snapshot.Resources = append(snapshot.Resources, discoveryResource(resource, domain.MetadataDiscovery))
	}

	crdByKey := make(map[string]kube.CustomResource, len(in.customs))
	for _, custom := range in.customs {
		crdByKey[custom.Group+"/"+custom.Plural] = custom
	}

	fallbackBuiltins := len(in.discovery.Resources) == 0
	if fallbackBuiltins {
		snapshot.Complete = false
		if len(snapshot.Issues) == 0 {
			snapshot.Issues = append(snapshot.Issues, domain.DiscoveryIssue{
				Code:    "discovery_unavailable",
				Message: "API discovery did not return any resource types; built-in navigation is unverified",
			})
		}
	}

	out := make([]domain.KindInfo, 0, len(domain.Catalogue())+len(in.discovery.Resources))
	seenCustom := make(map[string]struct{})

	for _, info := range domain.Catalogue() {
		key := info.Group + "/" + info.Resource
		served, ok := discovered[key]
		if !ok && !fallbackBuiltins {
			continue
		}
		entry := info
		entry.MetadataSource = domain.MetadataBuiltin
		if ok {
			entry.Version = served.Version
			entry.Verbs = append([]string(nil), served.Verbs...)
			entry = withDiscoveryResource(entry, served)
			markSnapshotSource(&snapshot, served, domain.MetadataBuiltin)
		} else {
			entry.Unverified = true
		}
		out = append(out, entry)
	}

	for _, served := range in.discovery.Resources {
		if builtinKind(served) {
			continue
		}
		key := served.Group + "/" + served.Resource
		if _, seen := seenCustom[key]; seen {
			continue
		}
		seenCustom[key] = struct{}{}

		if custom, ok := crdByKey[key]; ok {
			entry := customKindInfo(enrichCustom(custom, served))
			entry.MetadataSource = domain.MetadataCRD
			entry.Version = served.Version
			entry.Verbs = append([]string(nil), served.Verbs...)
			out = append(out, entry)
			markSnapshotSource(&snapshot, served, domain.MetadataCRD)
			continue
		}

		entry := discoveryKindInfo(served)
		entry.Verbs = append([]string(nil), served.Verbs...)
		out = append(out, entry)
		markSnapshotSource(&snapshot, served, domain.MetadataDiscovery)
	}

	for _, custom := range in.customs {
		key := custom.Group + "/" + custom.Plural
		if _, seen := seenCustom[key]; seen {
			continue
		}
		if !fallbackBuiltins {
			if _, ok := discovered[key]; !ok {
				continue
			}
		}
		entry := customKindInfo(custom)
		entry.MetadataSource = domain.MetadataCRD
		if served, ok := discovered[key]; ok {
			entry.Version = served.Version
			entry.Verbs = append([]string(nil), served.Verbs...)
			markSnapshotSource(&snapshot, served, domain.MetadataCRD)
			snapshot.Resources = append(snapshot.Resources, discoveryResource(served, domain.MetadataCRD))
		}
		out = append(out, entry)
	}

	sortDiscoveryResources(snapshot.Resources)
	return out, snapshot
}

func enrichCustom(custom kube.CustomResource, served kube.APIResource) kube.CustomResource {
	if custom.Version == served.Version {
		return custom
	}
	for _, version := range custom.ServedVersions {
		if version == served.Version {
			custom.Version = served.Version
			return custom
		}
	}
	custom.Version = served.Version
	return custom
}

func discoveryKindInfo(served kube.APIResource) domain.KindInfo {
	title := served.Resource
	if served.Kind != "" {
		title = plural(served.Kind)
	}
	return domain.KindInfo{
		Kind:           domain.CustomKind(served.Resource, served.Group),
		Title:          title,
		Category:       domain.CategoryCustom,
		Group:          served.Group,
		Version:        served.Version,
		Resource:       served.Resource,
		Namespaced:     served.Namespaced,
		Custom:         true,
		MetadataSource: domain.MetadataDiscovery,
	}
}

func discoveryResource(served kube.APIResource, source domain.MetadataSource) domain.DiscoveryResource {
	return domain.DiscoveryResource{
		Group:            served.Group,
		Version:          served.Version,
		Resource:         served.Resource,
		Kind:             served.Kind,
		Namespaced:       served.Namespaced,
		SupportedVerbs:   append([]string(nil), served.Verbs...),
		PreferredVersion: served.Version,
		MetadataSource:   source,
	}
}

func withDiscoveryResource(info domain.KindInfo, served kube.APIResource) domain.KindInfo {
	info.Version = served.Version
	return info
}

func markSnapshotSource(snapshot *domain.DiscoverySnapshot, served kube.APIResource, source domain.MetadataSource) {
	key := served.Group + "/" + served.Resource
	for i := range snapshot.Resources {
		if snapshot.Resources[i].Group+"/"+snapshot.Resources[i].Resource != key {
			continue
		}
		snapshot.Resources[i].MetadataSource = source
		return
	}
}

func sortDiscoveryResources(resources []domain.DiscoveryResource) {
	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Group != resources[j].Group {
			return resources[i].Group < resources[j].Group
		}
		return resources[i].Resource < resources[j].Resource
	})
}

func builtinKind(served kube.APIResource) bool {
	for _, info := range domain.Catalogue() {
		if info.Group == served.Group && info.Resource == served.Resource {
			return true
		}
	}
	return false
}

func crdIssueCode(err error) string {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "forbidden"):
		return "forbidden"
	case strings.Contains(msg, "unauthorized"):
		return "unauthorized"
	default:
		return "unavailable"
	}
}

// catalogueFor builds the navigation one cluster serves.
//
// Deprecated: use buildCatalogue for discovery-aware merges. catalogueFor
// remains for tests that predate the incident workspace work.
func catalogueFor(served []kube.APIResource, customs []kube.CustomResource) []domain.KindInfo {
	catalogue, _ := BuildCatalogue(CatalogueInput{
		Discovery: kube.DiscoveryResult{
			Resources: served,
			Complete:  len(served) > 0,
		},
		Customs: customs,
	})
	return catalogue
}

func kindUnavailable(clusterID string, kind domain.Kind, snapshot domain.DiscoverySnapshot) error {
	return fmt.Errorf("resource type %q is unavailable in cluster %q (session %s)", kind, clusterID, snapshot.SessionEpoch)
}
