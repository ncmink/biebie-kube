package cluster

import (
	"testing"

	bctx "github.com/ncmink/biebie-protocol/context"

	"biebie-kube/internal/store"
)

// A cluster whose kind went missing is still a production cluster, and reading
// it as unclassified is what takes away the band, the badge and the typed
// confirmation standing between a tired operator and a customer's workload.
func TestEnvironmentIsRecoveredFromTheWordsStoredBesideIt(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record store.ClusterRecord
		want   bctx.Environment
	}{
		{
			name:   "kind stored outright is used as it is",
			record: store.ClusterRecord{EnvironmentKind: "staging"},
			want:   bctx.EnvironmentStaging,
		},
		{
			name:   "the id says production",
			record: store.ClusterRecord{EnvironmentID: "production", EnvironmentName: "Production"},
			want:   bctx.EnvironmentProduction,
		},
		{
			name:   "the name alone says development",
			record: store.ClusterRecord{EnvironmentName: "Development"},
			want:   bctx.EnvironmentDevelopment,
		},
		{
			name:   "a stored kind is preferred over the words beside it",
			record: store.ClusterRecord{EnvironmentKind: "development", EnvironmentName: "Production"},
			want:   bctx.EnvironmentDevelopment,
		},
		{
			name:   "somebody's own vocabulary is not guessed at",
			record: store.ClusterRecord{EnvironmentID: "prod", EnvironmentName: "UAT"},
			want:   bctx.EnvironmentUnknown,
		},
		{
			name:   "nothing stored stays unclassified",
			record: store.ClusterRecord{},
			want:   bctx.EnvironmentUnknown,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fromRecord(tc.record).EnvironmentKind; got != tc.want {
				t.Fatalf("kind = %q, want %q", got, tc.want)
			}
		})
	}
}