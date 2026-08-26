package cluster

import (
	"errors"
	"strings"
	"testing"

	"biebie-kube/internal/domain"
)

// TestClassifyNamesTheCredentialHelper covers the failure that reads as a
// network problem but is not one. An EKS kubeconfig says `command: aws`, and a
// GUI application on macOS inherits launchd's PATH rather than the user's, so
// the helper cannot be found and no request is ever sent. Classifying that as
// FailureAPIUnavailable told the user "the cluster could not be reached" and
// sent them to check their VPN.
func TestClassifyNamesTheCredentialHelper(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantKind domain.FailureKind
		wantIn   string
	}{
		{
			name: "helper missing, as client-go reports it through a request error",
			err: errors.New(`Get "https://example.gr7.ap-southeast-1.eks.amazonaws.com/version?timeout=30s": ` +
				`getting credentials: exec: executable aws not found`),
			wantKind: domain.FailureCredentialHelper,
			wantIn:   `"aws"`,
		},
		{
			name: "helper missing, with the install hint client-go appends once",
			err: errors.New("getting credentials: exec: executable gke-gcloud-auth-plugin not found\n\n" +
				"It looks like you are trying to use a client-go credential plugin that is not installed."),
			wantKind: domain.FailureCredentialHelper,
			wantIn:   `"gke-gcloud-auth-plugin"`,
		},
		{
			// The exit code message also ends in a word that the missing-binary
			// pattern could match, so the two must not be confused: an expired
			// SSO session is a different fix from a missing install.
			name:     "helper ran and failed",
			err:      errors.New("getting credentials: exec: executable aws failed with exit code 253"),
			wantKind: domain.FailureCredentialHelper,
			wantIn:   "did not return a token",
		},
		{
			name:     "an ordinary unreachable cluster is untouched",
			err:      errors.New("dial tcp 10.0.0.1:443: connect: connection refused"),
			wantKind: domain.FailureAPIUnavailable,
			wantIn:   "refused the connection",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			kind, summary := classify(test.err)
			if kind != test.wantKind {
				t.Errorf("kind = %q, want %q", kind, test.wantKind)
			}
			if !strings.Contains(summary, test.wantIn) {
				t.Errorf("summary = %q, want it to contain %q", summary, test.wantIn)
			}
		})
	}
}

// TestCredentialHelperFailureIsNotBlamedOnTheCluster checks the state the
// session lands in. Unreachable would put the fix on the cluster owner's side
// of the handover, when the machine running Biebie Kube is what needs changing.
func TestCredentialHelperFailureIsNotBlamedOnTheCluster(t *testing.T) {
	if got := stateFor(domain.FailureCredentialHelper); got != domain.ClusterFailed {
		t.Errorf("stateFor(FailureCredentialHelper) = %q, want %q", got, domain.ClusterFailed)
	}
}
