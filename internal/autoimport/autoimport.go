// Package autoimport turns the contexts already in an engineer's kubeconfig
// into clusters Biebie Kube can open.
//
// Two rules shape everything here. The kubeconfig is read-only, as it is
// everywhere else in this application: contexts are a source of suggestions,
// never a place to write back. And a context is imported at most once — a
// cluster the engineer then deletes, renames or reclassifies is theirs, so a
// later scan must not recreate it or overwrite what they chose.
package autoimport

import (
	"regexp"
	"sort"
	"strings"
	"time"

	bctx "biebie-kube/protocol/context"

	"biebie-kube/internal/cluster"
	"biebie-kube/internal/domain"
	"biebie-kube/internal/kubeconfig"
	"biebie-kube/internal/store"
)

// ContextSource lists the indexed kubeconfigs and the contexts inside them.
//
// It is an interface so the rules below can be exercised without a kubeconfig
// on disk, which is what makes the classification table cheap to test.
type ContextSource interface {
	List() []kubeconfig.File
}

// Service scans kubeconfigs and creates the clusters they imply.
type Service struct {
	configs  ContextSource
	clusters *cluster.Repository
	store    *store.Store

	now func() time.Time
}

// NewService wires the scanner to the kubeconfig index and cluster storage.
func NewService(configs ContextSource, clusters *cluster.Repository, st *store.Store) *Service {
	return &Service{configs: configs, clusters: clusters, store: st, now: time.Now}
}

// Candidate is a kubeconfig context that has no cluster of its own yet.
type Candidate struct {
	KubeconfigRef  string `json:"kubeconfigRef"`
	KubeconfigName string `json:"kubeconfigName"`

	ContextName string `json:"contextName"`
	Server      string `json:"server"`

	// Name and EnvironmentKind are what an import would use. They are shown
	// before anything is created, so the guess is visible rather than implied.
	Name            string           `json:"name"`
	EnvironmentKind bctx.Environment `json:"environmentKind"`

	// Seen marks a context automatic import has already handled once. Such a
	// context stays available to import by hand, but is never added again on
	// its own, because its absence is a decision the engineer made.
	Seen bool `json:"seen"`
}

// Result reports what an import did.
type Result struct {
	Added  []domain.Cluster `json:"added"`
	Failed []Failure        `json:"failed"`
}

// Failure is one context that could not become a cluster, kept alongside the
// successes so a partial import is reported honestly.
type Failure struct {
	ContextName string `json:"contextName"`
	Reason      string `json:"reason"`
}

// Enabled reports whether new contexts are imported without being asked.
func (s *Service) Enabled() bool { return s.store.Read().AutoImportEnabled() }

// SetEnabled records the engineer's choice about automatic import.
func (s *Service) SetEnabled(enabled bool) error {
	return s.store.Update(func(data *store.Data) error {
		data.AutoImport = &enabled
		return nil
	})
}

// Scan lists every context across every indexed kubeconfig that is not already
// a cluster.
//
// A file that cannot be read contributes nothing rather than failing the scan:
// one broken kubeconfig must not hide the contexts in the others.
func (s *Service) Scan() []Candidate {
	existing := make(map[contextKey]struct{})
	for _, existingCluster := range s.clusters.All() {
		existing[contextKey{existingCluster.KubeconfigRef, existingCluster.ContextName}] = struct{}{}
	}

	seen := make(map[contextKey]struct{})
	for _, record := range s.store.Read().SeenContexts {
		seen[contextKey{record.KubeconfigRef, record.ContextName}] = struct{}{}
	}

	var candidates []Candidate
	for _, file := range s.configs.List() {
		for _, entry := range file.Contexts {
			key := contextKey{file.Ref, entry.Name}
			if _, taken := existing[key]; taken {
				continue
			}
			_, alreadySeen := seen[key]
			candidates = append(candidates, Candidate{
				KubeconfigRef:   file.Ref,
				KubeconfigName:  file.Name,
				ContextName:     entry.Name,
				Server:          entry.Server,
				Name:            entry.Name,
				EnvironmentKind: Classify(entry.Name, entry.Cluster, entry.Server),
				Seen:            alreadySeen,
			})
		}
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].KubeconfigName != candidates[j].KubeconfigName {
			return candidates[i].KubeconfigName < candidates[j].KubeconfigName
		}
		return candidates[i].ContextName < candidates[j].ContextName
	})
	return candidates
}

// Sync adds clusters for contexts that have never been offered before.
//
// This is the automatic path, run at startup. It does nothing when the engineer
// has switched automatic import off, and it ignores contexts it has already
// handled, so deleting an auto-imported cluster makes it stay deleted.
func (s *Service) Sync() Result {
	if !s.Enabled() {
		return Result{}
	}

	var fresh []Candidate
	for _, candidate := range s.Scan() {
		if !candidate.Seen {
			fresh = append(fresh, candidate)
		}
	}
	return s.importAll(fresh)
}

// ImportAll adds a cluster for every context that does not have one, including
// contexts a previous automatic run passed over. It is the explicit action
// behind the button, so nothing is skipped on the grounds of having been seen.
func (s *Service) ImportAll() Result { return s.importAll(s.Scan()) }

func (s *Service) importAll(candidates []Candidate) Result {
	result := Result{}
	for _, candidate := range candidates {
		created, err := s.clusters.Create(candidate.input(), candidate.Server)
		if err != nil {
			result.Failed = append(result.Failed, Failure{
				ContextName: candidate.ContextName,
				Reason:      err.Error(),
			})
			continue
		}
		result.Added = append(result.Added, created)
	}

	// Every context that was considered is marked, including the ones that
	// failed, so a context that cannot become a cluster is not retried at each
	// launch. A failure to record this is harmless: cluster creation already
	// refuses a duplicate context, so the worst case is one repeated attempt.
	_ = s.markSeen(candidates)

	return result
}

func (s *Service) markSeen(candidates []Candidate) error {
	if len(candidates) == 0 {
		return nil
	}
	stamp := s.now().UTC().Format(time.RFC3339)

	return s.store.Update(func(data *store.Data) error {
		known := make(map[contextKey]struct{}, len(data.SeenContexts))
		for _, record := range data.SeenContexts {
			known[contextKey{record.KubeconfigRef, record.ContextName}] = struct{}{}
		}
		for _, candidate := range candidates {
			key := contextKey{candidate.KubeconfigRef, candidate.ContextName}
			if _, ok := known[key]; ok {
				continue
			}
			known[key] = struct{}{}
			data.SeenContexts = append(data.SeenContexts, store.SeenContextRecord{
				KubeconfigRef: candidate.KubeconfigRef,
				ContextName:   candidate.ContextName,
				SeenAt:        stamp,
			})
		}
		return nil
	})
}

// input describes the cluster this candidate would create.
//
// Customer is left blank and Biebie Access is left out of it: a kubeconfig says
// nothing about which customer owns a cluster or which VPN reaches it, and
// inventing either would be worse than leaving the engineer to fill it in.
func (c Candidate) input() domain.ClusterInput {
	input := domain.ClusterInput{
		Name:            c.Name,
		EnvironmentKind: c.EnvironmentKind,
		KubeconfigRef:   c.KubeconfigRef,
		ContextName:     c.ContextName,
	}
	if c.EnvironmentKind != bctx.EnvironmentUnknown {
		kind := string(c.EnvironmentKind)
		input.EnvironmentID = kind
		input.EnvironmentName = strings.ToUpper(kind[:1]) + kind[1:]
	}
	return input
}

type contextKey struct {
	kubeconfigRef string
	contextName   string
}

var separators = regexp.MustCompile(`[^a-z0-9]+`)

// Classify guesses which environment a context belongs to from the names
// around it, trying each in the order given and taking the first opinion.
//
// It is deliberately unwilling to guess. An unclassified cluster costs the
// engineer one edit; a production cluster labelled development would drop the
// warnings and the typed confirmation that stand between a tired operator and
// a deleted customer workload.
func Classify(names ...string) bctx.Environment {
	for _, name := range names {
		if kind := classifyOne(name); kind != bctx.EnvironmentUnknown {
			return kind
		}
	}
	return bctx.EnvironmentUnknown
}

func classifyOne(name string) bctx.Environment {
	lower := strings.ToLower(strings.TrimSpace(name))
	if lower == "" {
		return bctx.EnvironmentUnknown
	}

	// Separators are dropped for the compound checks so that "pre-prod",
	// "pre_prod" and "preprod" are one case rather than three.
	squashed := separators.ReplaceAllString(lower, "")

	// "nonprod" and "non-prod" say only what a cluster is not, so they stop the
	// production rule below from reading them the wrong way round.
	if strings.Contains(squashed, "nonprod") {
		return bctx.EnvironmentUnknown
	}
	if strings.Contains(squashed, "preprod") {
		return bctx.EnvironmentStaging
	}

	// Whole words only. A customer called Devon or a cluster named "reproduce"
	// must not be read as a development or production marker.
	words := make(map[string]struct{})
	for _, word := range separators.Split(lower, -1) {
		if word != "" {
			words[word] = struct{}{}
		}
	}

	switch {
	case anyOf(words, "staging", "stage", "stg", "uat", "sit"):
		return bctx.EnvironmentStaging
	case anyOf(words, "production", "prod", "prd", "live"):
		return bctx.EnvironmentProduction
	case anyOf(words, "development", "develop", "dev", "test", "testing", "qa",
		"sandbox", "sbx", "local", "minikube", "kind", "k3d", "k3s", "colima", "orbstack"):
		return bctx.EnvironmentDevelopment
	case both(words, "docker", "desktop"), both(words, "rancher", "desktop"):
		// A desktop cluster is somebody's laptop, whatever it is called.
		return bctx.EnvironmentDevelopment
	default:
		return bctx.EnvironmentUnknown
	}
}

func anyOf(words map[string]struct{}, candidates ...string) bool {
	for _, candidate := range candidates {
		if _, ok := words[candidate]; ok {
			return true
		}
	}
	return false
}

func both(words map[string]struct{}, first, second string) bool {
	_, hasFirst := words[first]
	_, hasSecond := words[second]
	return hasFirst && hasSecond
}
