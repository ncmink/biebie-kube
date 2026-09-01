package gitops

import (
	"strings"
	"testing"

	"biebie-kube/internal/domain"
)

func TestASourceThatRendersItsObjectsIsUnavailableRatherThanFailed(t *testing.T) {
	// Helm will never have a file that equals a running object. Colouring that
	// red would teach people that the panel is broken, when what it is doing
	// is declining to guess.
	search := domain.ManifestSearch{Certainty: domain.ManifestGenerated, Reason: "Helm renders this object from templates."}

	got := compareAgainst(search, nil, refusal{})
	if got.State != domain.ComparisonUnavailable {
		t.Fatalf("state = %q", got.State)
	}
	if got.Blocker != domain.BlockerGenerated {
		t.Fatalf("blocker = %q", got.Blocker)
	}
	if got.Reason != search.Reason {
		t.Fatalf("reason = %q, which is not what the source said", got.Reason)
	}
}

func TestEachWayAComparisonCanDeclineIsItsOwnState(t *testing.T) {
	// Collapsing these into one "diff failed" would tell a person to go and do
	// the wrong thing: a repository that could not be reached is worth another
	// press, and two matching files is worth going and looking at the
	// repository.
	located := &domain.ManifestLocation{Path: "apps/deployment.yaml", Content: "kind: Deployment\n"}

	for name, test := range map[string]struct {
		search  domain.ManifestSearch
		refused refusal
		want    domain.ComparisonBlocker
	}{
		"ambiguous": {
			search: domain.ManifestSearch{
				Certainty:  domain.ManifestTree,
				Candidates: []domain.ManifestLocation{*located, *located},
			},
			want: domain.BlockerAmbiguous,
		},
		"repository": {
			search:  domain.ManifestSearch{Certainty: domain.ManifestTree, Reason: "Git could not authenticate."},
			refused: refusal{message: "Git could not authenticate."},
			want:    domain.BlockerRepository,
		},
		"nothing found": {
			search: domain.ManifestSearch{
				Certainty: domain.ManifestTree,
				Reason:    "Nothing in the 12 files under this path declares this object.",
			},
			want: domain.BlockerNotLocated,
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := compareAgainst(test.search, nil, test.refused)
			if got.State != domain.ComparisonUnavailable {
				t.Fatalf("state = %q", got.State)
			}
			if got.Blocker != test.want {
				t.Fatalf("blocker = %q, want %q", got.Blocker, test.want)
			}
			if got.Reason == "" {
				t.Fatal("a state with no sentence leaves the panel with nothing to say")
			}
		})
	}
}

func TestAManifestThatDoesNotParseSaysWhichFileAndWhy(t *testing.T) {
	// This one is worth going and fixing, so the message names the file rather
	// than saying the comparison did not work.
	search := domain.ManifestSearch{
		Certainty: domain.ManifestExact,
		Located:   &domain.ManifestLocation{Path: "apps/deployment.yaml", Content: "spec:\n  replicas: [\n"},
	}

	got := compareAgainst(search, subjectFor(t, `metadata: {name: payment-api}`, false), refusal{})
	if got.Blocker != domain.BlockerManifestInvalid {
		t.Fatalf("blocker = %q", got.Blocker)
	}
	if !strings.Contains(got.Reason, "apps/deployment.yaml") {
		t.Fatalf("the reason does not name the file: %q", got.Reason)
	}
}

func TestAnObjectEqualToItsManifestIsNotTheSameStateAsOneWithNoManifest(t *testing.T) {
	// The distinction the whole result model exists for. Both have an empty
	// difference list, and they mean opposite things.
	equal := compareAgainst(domain.ManifestSearch{
		Certainty: domain.ManifestExact,
		Located: &domain.ManifestLocation{
			Path:    "apps/deployment.yaml",
			Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: payment-api}\n",
		},
	}, subjectFor(t, `
apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api}
`, false), refusal{})

	if equal.State != domain.ComparisonEqual {
		t.Fatalf("state = %q: %s", equal.State, equal.Reason)
	}
	if len(equal.Differences) != 0 {
		t.Fatalf("differences = %+v", equal.Differences)
	}

	missing := compareAgainst(
		domain.ManifestSearch{Certainty: domain.ManifestTree, Reason: "nothing found"}, nil, refusal{})
	if missing.State == equal.State {
		t.Fatal("an object with no manifest reads the same as one that matches its manifest")
	}
}

func TestTheComparisonReadsOnlyTheDocumentItWasHanded(t *testing.T) {
	// This is what pins the answer to one commit. compareAgainst takes no
	// context and no repository, so it cannot go back to Git and read a branch
	// that has moved since the manifest on screen was fetched — the content it
	// compares is the content the search returned, and there is no second
	// resolution for the two to disagree about.
	//
	// It is also what makes a multi-document file behave: the located document
	// is one document, and the rest of the file is not in the comparison
	// because it is not in the string.
	search := domain.ManifestSearch{
		Certainty: domain.ManifestExact,
		Commit:    "8f19cfa1b2c3d4e5f60718293a4b5c6d7e8f9012",
		Located: &domain.ManifestLocation{
			Path:     "apps/payment/prod/resources.yaml",
			Document: 2,
			Content: "apiVersion: apps/v1\nkind: Deployment\n" +
				"metadata: {name: payment-api}\nspec: {replicas: 5}\n",
		},
	}
	live := subjectFor(t, `
apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api}
spec: {replicas: 3}
`, false)

	got := compareAgainst(search, live, refusal{})
	if got.State != domain.ComparisonDiffers || len(got.Differences) != 1 {
		t.Fatalf("comparison = %+v", got)
	}
	if got.Differences[0].Source != "5" {
		// A comparison against the whole file, or against a re-read of the
		// branch, would not produce this number.
		source := got.Differences[0].Source
		t.Fatalf("source = %q, which is not what the located document says", source)
	}
}

func TestASecretComparisonSaysItWithheldSomething(t *testing.T) {
	// Blank values that nothing explains look like a bug. The flag is what
	// lets the panel say the field differs and the value is not being shown.
	search := domain.ManifestSearch{
		Certainty: domain.ManifestExact,
		Located: &domain.ManifestLocation{
			Path:    "apps/secret.yaml",
			Content: "apiVersion: v1\nkind: Secret\nmetadata: {name: api-credentials}\ndata: {TOKEN: ZGVzaXJlZA==}\n",
		},
	}
	live := subjectFor(t, `
apiVersion: v1
kind: Secret
metadata: {name: api-credentials}
data: {TOKEN: bGl2ZQ==}
`, true)

	got := compareAgainst(search, live, refusal{})
	if !got.Redacted {
		t.Fatal("a comparison that withheld a value did not say so")
	}
	for _, difference := range got.Differences {
		if strings.Contains(difference.Source+difference.Live, "ZGVzaXJlZA") {
			t.Fatalf("a secret value crossed the boundary: %+v", difference)
		}
	}
}

func TestSystemManagedFieldsAloneDoNotCountAsDifferingButAreNotCalledIdentical(t *testing.T) {
	// The state that most of a cluster is in. Nothing is wrong, and the object
	// is not identical to its manifest either — it carries the annotation its
	// own controller wrote. Saying "identical" would be a small untruth, and a
	// small untruth is what costs a reader their trust in the rest of the
	// panel.
	search := domain.ManifestSearch{
		Certainty: domain.ManifestExact,
		Located: &domain.ManifestLocation{
			Path:    "apps/deployment.yaml",
			Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: ak-super-auto}\n",
		},
	}
	live := subjectFor(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ak-super-auto
  annotations: {deployment.kubernetes.io/revision: "22"}
  labels: {argocd.argoproj.io/instance: ak-super-auto}
`, false)

	got := compareAgainst(search, live, refusal{})
	if got.State != domain.ComparisonEqual {
		t.Fatalf("state = %q: %s", got.State, got.Reason)
	}
	if got.Meaningful != 0 || got.SystemManaged != 2 {
		t.Fatalf("meaningful = %d, system-managed = %d", got.Meaningful, got.SystemManaged)
	}
	// Kept rather than discarded: an engineer working out why a rollout
	// happened wants the revision, and it is one disclosure away.
	if len(got.Differences) != 2 {
		t.Fatalf("system-managed differences were thrown away: %+v", got.Differences)
	}
	if strings.Contains(got.Reason, "identical") || strings.Contains(got.Reason, "matches") {
		t.Fatalf("reason overclaims: %q", got.Reason)
	}
}

func TestMeaningfulDifferencesComeFirst(t *testing.T) {
	// The summary leads with what to look at, so the list has to as well.
	search := domain.ManifestSearch{
		Certainty: domain.ManifestExact,
		Located: &domain.ManifestLocation{
			Path: "apps/deployment.yaml",
			Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: ak-super-auto}\n" +
				"spec: {template: {spec: {containers: [{name: api, image: \"api:v1.9\"}]}}}\n",
		},
	}
	live := subjectFor(t, `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ak-super-auto
  annotations: {deployment.kubernetes.io/revision: "22"}
spec: {template: {spec: {containers: [{name: api, image: "api:v1.8"}]}}}
`, false)

	got := compareAgainst(search, live, refusal{})
	if got.Meaningful != 1 || got.SystemManaged != 1 {
		t.Fatalf("meaningful = %d, system-managed = %d: %+v", got.Meaningful, got.SystemManaged, got.Differences)
	}
	if got.Differences[0].Class != domain.DifferenceMeaningful {
		t.Fatalf("the list leads with %+v", got.Differences[0])
	}
	if got.Differences[0].Label != "Container image" {
		t.Fatalf("label = %q", got.Differences[0].Label)
	}
}

func TestTheCountInTheReasonMatchesTheDifferences(t *testing.T) {
	search := domain.ManifestSearch{
		Certainty: domain.ManifestExact,
		Located: &domain.ManifestLocation{
			Path:    "apps/deployment.yaml",
			Content: "apiVersion: apps/v1\nkind: Deployment\nmetadata: {name: payment-api}\nspec: {replicas: 5, paused: true}\n",
		},
	}
	live := subjectFor(t, `
apiVersion: apps/v1
kind: Deployment
metadata: {name: payment-api}
spec: {replicas: 3, paused: false}
`, false)

	got := compareAgainst(search, live, refusal{})
	if !strings.HasPrefix(got.Reason, "2 differences") {
		t.Fatalf("reason = %q, differences = %d", got.Reason, len(got.Differences))
	}
}

func subjectFor(t *testing.T, document string, sensitive bool) *subject {
	t.Helper()
	return &subject{object: object(t, document), sensitive: sensitive}
}
