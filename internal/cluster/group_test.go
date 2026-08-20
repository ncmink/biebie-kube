package cluster

import (
	"testing"

	bctx "biebie-kube/protocol/context"

	"biebie-kube/internal/domain"
	"biebie-kube/internal/store"
)

func addFor(t *testing.T, repo *Repository, customerID, customerName, name, contextName string) domain.Cluster {
	t.Helper()
	return addCluster(t, repo, domain.ClusterInput{
		Name:            name,
		CustomerID:      customerID,
		CustomerName:    customerName,
		EnvironmentKind: bctx.EnvironmentDevelopment,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     contextName,
	}, "https://cluster.example:6443")
}

func groupFor(t *testing.T, repo *Repository, key string) domain.CustomerGroup {
	t.Helper()
	for _, group := range repo.Groups() {
		if group.Key == key {
			return group
		}
	}
	t.Fatalf("no group %q in %+v", key, repo.Groups())
	return domain.CustomerGroup{}
}

// populated drops the sections the dashboard would not draw, which is how the
// always-present archive stays out of tests that are about customers.
func populated(groups []domain.CustomerGroup) []domain.CustomerGroup {
	out := make([]domain.CustomerGroup, 0, len(groups))
	for _, group := range groups {
		if len(group.ClusterIDs) > 0 {
			out = append(out, group)
		}
	}
	return out
}

func TestGroupsCollectClustersPerCustomerAndPutTheUnnamedOnesLast(t *testing.T) {
	repo := newRepo(t)
	addFor(t, repo, "zeta", "Zeta Co", "Staging", "zeta-staging")
	addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")
	addFor(t, repo, "smoi", "SMOI", "Sandbox", "smoi-sandbox")
	addFor(t, repo, "", "", "Docker Desktop", "docker-desktop")

	groups := populated(repo.Groups())
	if len(groups) != 3 {
		t.Fatalf("expected 3 groups, got %d: %+v", len(groups), groups)
	}
	if groups[0].Key != "smoi" || len(groups[0].ClusterIDs) != 2 {
		t.Fatalf("expected SMOI with 2 clusters first, got %+v", groups[0])
	}
	if groups[1].Key != "zeta" {
		t.Fatalf("expected Zeta Co second, got %+v", groups[1])
	}
	if groups[2].Key != "" || groups[2].Label != domain.UngroupedLabel {
		t.Fatalf("expected the customerless group last, labelled %q, got %+v",
			domain.UngroupedLabel, groups[2])
	}
}

func TestHidingACustomerHidesEveryClusterUnderIt(t *testing.T) {
	repo := newRepo(t)
	addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")
	addFor(t, repo, "smoi", "SMOI", "Sandbox", "smoi-sandbox")
	addFor(t, repo, "zeta", "Zeta Co", "Staging", "zeta-staging")

	if err := repo.SetGroupHidden("smoi", true); err != nil {
		t.Fatalf("hide group: %v", err)
	}
	if !repo.GroupHidden("smoi") {
		t.Fatal("expected SMOI to be hidden")
	}
	if repo.GroupHidden("zeta") {
		t.Fatal("hiding one customer must not hide another")
	}

	group := groupFor(t, repo, "smoi")
	if !group.Hidden || len(group.ClusterIDs) != 2 {
		t.Fatalf("expected both SMOI clusters reported as hidden, got %+v", group)
	}
}

// Hiding is presentation: the clusters themselves must still be there, or a
// hidden group would be indistinguishable from a deleted one.
func TestHiddenClustersAreStillStoredAndReadable(t *testing.T) {
	repo := newRepo(t)
	cluster := addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")

	if err := repo.SetGroupHidden("smoi", true); err != nil {
		t.Fatalf("hide group: %v", err)
	}

	if len(repo.All()) != 1 {
		t.Fatalf("expected the cluster to survive hiding, got %+v", repo.All())
	}
	if _, err := repo.Get(cluster.ID); err != nil {
		t.Fatalf("expected a hidden cluster to still resolve: %v", err)
	}
	found, ok := repo.FindByContext(bctx.BiebieContext{CustomerID: "smoi", ClusterName: "RKE2"})
	if !ok || found.ID != cluster.ID {
		t.Fatal("expected a handoff to still match a cluster in a hidden group")
	}
}

func TestRevealingACustomerForgetsTheStoredFlag(t *testing.T) {
	repo := newRepo(t)
	addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")

	if err := repo.SetGroupHidden("smoi", true); err != nil {
		t.Fatalf("hide group: %v", err)
	}
	if err := repo.SetGroupHidden("smoi", false); err != nil {
		t.Fatalf("reveal group: %v", err)
	}
	if repo.GroupHidden("smoi") {
		t.Fatal("expected SMOI to be visible again")
	}
	if records := repo.store.Read().Customers; len(records) != 0 {
		t.Fatalf("a visible group should not be recorded on disk, got %+v", records)
	}
}

func TestMovingTheLastClusterToAnotherCustomerForgetsTheHiddenFlag(t *testing.T) {
	repo := newRepo(t)
	cluster := addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")
	if err := repo.SetGroupHidden("smoi", true); err != nil {
		t.Fatalf("hide group: %v", err)
	}

	if _, err := repo.Update(cluster.ID, domain.ClusterInput{
		Name:            "RKE2",
		CustomerID:      "other",
		CustomerName:    "Other Co",
		EnvironmentKind: bctx.EnvironmentDevelopment,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     "smoi-rke2",
	}, "https://cluster.example:6443"); err != nil {
		t.Fatalf("update cluster: %v", err)
	}

	if repo.GroupHidden("smoi") {
		t.Fatal("expected the emptied customer to lose its hidden flag")
	}
	addFor(t, repo, "smoi", "SMOI", "Sandbox", "smoi-sandbox")
	if repo.GroupHidden("smoi") {
		t.Fatal("a customer identifier used again must start visible")
	}
}

func TestDeletingTheLastClusterOfACustomerForgetsTheHiddenFlag(t *testing.T) {
	repo := newRepo(t)
	cluster := addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")
	if err := repo.SetGroupHidden("smoi", true); err != nil {
		t.Fatalf("hide group: %v", err)
	}
	if err := repo.Delete(cluster.ID); err != nil {
		t.Fatalf("delete cluster: %v", err)
	}
	if repo.GroupHidden("smoi") {
		t.Fatal("expected the deleted customer to lose its hidden flag")
	}
}

func TestHidingACustomerWithoutClustersIsRejected(t *testing.T) {
	repo := newRepo(t)
	addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")

	if err := repo.SetGroupHidden("nobody", true); err == nil {
		t.Fatal("expected hiding an unknown customer to fail")
	}
}

// Hiding the customerless clusters would hide the section that offers to reveal
// them, and auto-import puts every context it finds there.
func TestTheCustomerlessClustersCannotBeHidden(t *testing.T) {
	repo := newRepo(t)
	addFor(t, repo, "", "", "Docker Desktop", "docker-desktop")

	if err := repo.SetGroupHidden("", true); err == nil {
		t.Fatal("expected hiding the customerless clusters to be refused")
	}
	if repo.GroupHidden("") {
		t.Fatal("expected the customerless clusters to stay visible")
	}

	// A flag written by an older build must not strand them either.
	if err := repo.store.Update(func(data *store.Data) error {
		data.Customers = append(data.Customers, store.CustomerRecord{Hidden: true})
		return nil
	}); err != nil {
		t.Fatalf("write legacy flag: %v", err)
	}
	if repo.GroupHidden("") || groupFor(t, repo, "").Hidden {
		t.Fatal("expected a stored flag on the customerless clusters to be ignored")
	}
}

func TestTheArchiveIsAlwaysListedAndStartsHidden(t *testing.T) {
	repo := newRepo(t)
	addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")

	archive := groupFor(t, repo, domain.ArchivedKey)
	if !archive.Hidden || len(archive.ClusterIDs) != 0 {
		t.Fatalf("expected an empty, hidden archive, got %+v", archive)
	}
	if !repo.GroupHidden(domain.ArchivedKey) {
		t.Fatal("expected the archive to be hidden without anyone choosing")
	}
	if records := repo.store.Read().Customers; len(records) != 0 {
		t.Fatalf("a default needs no record on disk, got %+v", records)
	}

	// It is the last section, so putting a cluster away never pushes the
	// customers the engineer is working with further down the page.
	groups := repo.Groups()
	if groups[len(groups)-1].Key != domain.ArchivedKey {
		t.Fatalf("expected the archive last, got %+v", groups)
	}
}

// Hidden is the archive's default, so it is revealing it that has to be
// remembered — the opposite of a customer.
func TestRevealingTheArchiveIsRemembered(t *testing.T) {
	repo := newRepo(t)

	if err := repo.SetGroupHidden(domain.ArchivedKey, false); err != nil {
		t.Fatalf("reveal the archive: %v", err)
	}
	if repo.GroupHidden(domain.ArchivedKey) {
		t.Fatal("expected the archive to be visible")
	}
	records := repo.store.Read().Customers
	if len(records) != 1 || records[0].Key != domain.ArchivedKey || records[0].Hidden {
		t.Fatalf("expected a stored hidden=false for the archive, got %+v", records)
	}

	if err := repo.SetGroupHidden(domain.ArchivedKey, true); err != nil {
		t.Fatalf("hide the archive: %v", err)
	}
	if records := repo.store.Read().Customers; len(records) != 0 {
		t.Fatalf("back at the default, the record should be gone, got %+v", records)
	}
}

func TestArchivingMovesAClusterOutOfItsCustomerSection(t *testing.T) {
	repo := newRepo(t)
	cluster := addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")
	addFor(t, repo, "smoi", "SMOI", "Sandbox", "smoi-sandbox")

	archived, err := repo.SetArchived(cluster.ID, true)
	if err != nil {
		t.Fatalf("archive cluster: %v", err)
	}
	if !archived.Archived {
		t.Fatal("expected the cluster to report itself archived")
	}
	// The customer is unchanged, which is what lets it come back to SMOI.
	if archived.CustomerID != "smoi" || archived.CustomerName != "SMOI" {
		t.Fatalf("archiving must not rewrite the customer, got %+v", archived)
	}

	if archive := groupFor(t, repo, domain.ArchivedKey); len(archive.ClusterIDs) != 1 ||
		archive.ClusterIDs[0] != cluster.ID {
		t.Fatalf("expected the cluster in the archive, got %+v", archive)
	}
	if smoi := groupFor(t, repo, "smoi"); len(smoi.ClusterIDs) != 1 {
		t.Fatalf("expected SMOI to keep its other cluster only, got %+v", smoi)
	}
}

// An archived cluster is out of the list, not out of the application.
func TestArchivedClustersStillResolveAndAnswerHandoffs(t *testing.T) {
	repo := newRepo(t)
	cluster := addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")
	if _, err := repo.SetArchived(cluster.ID, true); err != nil {
		t.Fatalf("archive cluster: %v", err)
	}

	if len(repo.All()) != 1 {
		t.Fatalf("expected the cluster to survive archiving, got %+v", repo.All())
	}
	if _, err := repo.Get(cluster.ID); err != nil {
		t.Fatalf("expected an archived cluster to still resolve: %v", err)
	}
	found, ok := repo.FindByContext(bctx.BiebieContext{CustomerID: "smoi", ClusterName: "RKE2"})
	if !ok || found.ID != cluster.ID {
		t.Fatal("expected a handoff to still match an archived cluster")
	}
}

func TestTakingAClusterOutOfTheArchiveReturnsItToItsCustomer(t *testing.T) {
	repo := newRepo(t)
	cluster := addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")
	if err := repo.SetGroupHidden("smoi", true); err != nil {
		t.Fatalf("hide SMOI: %v", err)
	}
	if _, err := repo.SetArchived(cluster.ID, true); err != nil {
		t.Fatalf("archive cluster: %v", err)
	}

	// SMOI has no cluster on the list while its only one is put away, and its
	// choice must survive that or unarchiving would reveal a customer the
	// engineer had hidden.
	if !repo.GroupHidden("smoi") {
		t.Fatal("expected SMOI to still be hidden while its cluster is archived")
	}

	restored, err := repo.SetArchived(cluster.ID, false)
	if err != nil {
		t.Fatalf("unarchive cluster: %v", err)
	}
	if restored.Archived || restored.GroupKey() != "smoi" {
		t.Fatalf("expected the cluster back under SMOI, got %+v", restored)
	}
	if smoi := groupFor(t, repo, "smoi"); !smoi.Hidden || len(smoi.ClusterIDs) != 1 {
		t.Fatalf("expected SMOI hidden with its cluster back, got %+v", smoi)
	}
}

func TestARevealedArchiveSurvivesItsLastClusterLeaving(t *testing.T) {
	repo := newRepo(t)
	cluster := addFor(t, repo, "smoi", "SMOI", "RKE2", "smoi-rke2")
	if _, err := repo.SetArchived(cluster.ID, true); err != nil {
		t.Fatalf("archive cluster: %v", err)
	}
	if err := repo.SetGroupHidden(domain.ArchivedKey, false); err != nil {
		t.Fatalf("reveal the archive: %v", err)
	}

	if _, err := repo.SetArchived(cluster.ID, false); err != nil {
		t.Fatalf("unarchive cluster: %v", err)
	}
	if repo.GroupHidden(domain.ArchivedKey) {
		t.Fatal("expected the revealed archive to stay revealed once emptied")
	}
}

// Editing a cluster goes through the form, which knows nothing about the
// archive or about labels an import wrote. Neither may be lost.
func TestEditingAnArchivedClusterKeepsItArchivedAndKeepsItsLabels(t *testing.T) {
	repo := newRepo(t)
	cluster := addCluster(t, repo, domain.ClusterInput{
		Name:            "RKE2",
		CustomerID:      "smoi",
		CustomerName:    "SMOI",
		EnvironmentKind: bctx.EnvironmentDevelopment,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     "smoi-rke2",
		Labels:          map[string]string{"biebie.net/cluster-id": "smoi-rke2-prod"},
	}, "https://cluster.example:6443")
	if _, err := repo.SetArchived(cluster.ID, true); err != nil {
		t.Fatalf("archive cluster: %v", err)
	}

	edited, err := repo.Update(cluster.ID, domain.ClusterInput{
		Name:            "RKE2 production",
		CustomerID:      "smoi",
		CustomerName:    "SMOI",
		EnvironmentKind: bctx.EnvironmentProduction,
		KubeconfigRef:   "kubeconfig_1",
		ContextName:     "smoi-rke2",
	}, "https://cluster.example:6443")
	if err != nil {
		t.Fatalf("update cluster: %v", err)
	}
	if !edited.Archived {
		t.Fatal("expected the cluster to stay archived across an edit")
	}
	if edited.Labels["biebie.net/cluster-id"] != "smoi-rke2-prod" {
		t.Fatalf("expected the import's labels to survive an edit, got %+v", edited.Labels)
	}
}

// A customer named but not identified still needs a stable group, because the
// dashboard has to put those clusters somewhere.
func TestCustomerNameActsAsTheKeyWhenNoIdentifierWasGiven(t *testing.T) {
	repo := newRepo(t)
	addFor(t, repo, "", "Acme Co", "Staging", "acme-staging")

	group := groupFor(t, repo, "Acme Co")
	if group.Label != "Acme Co" {
		t.Fatalf("expected the name as both key and label, got %+v", group)
	}
	if err := repo.SetGroupHidden("Acme Co", true); err != nil {
		t.Fatalf("hide group: %v", err)
	}
	if !repo.GroupHidden("Acme Co") {
		t.Fatal("expected the named group to be hidden")
	}
}
