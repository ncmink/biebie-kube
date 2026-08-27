package resources

import (
	"fmt"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// pods builds a namespace's worth of pods, oldest last.
//
// The ages are spread a second apart so the default order — newest first — is
// unambiguous, which is what lets a test say which window a name falls in.
func pods(t *testing.T, count int) []*unstructured.Unstructured {
	t.Helper()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]*unstructured.Unstructured, 0, count)
	for i := 0; i < count; i++ {
		created := base.Add(-time.Duration(i) * time.Second).Format(time.RFC3339)
		out = append(out, object(t, fmt.Sprintf(`{
			"metadata": {
				"name": "api-%05d",
				"namespace": "shop",
				"uid": "uid-%05d",
				"creationTimestamp": %q
			},
			"spec": {"nodeName": "node-1", "containers": [{"name": "api"}]},
			"status": {"phase": "Running", "containerStatuses": [
				{"name": "api", "ready": true, "restartCount": %d, "state": {"running": {}}}
			]}
		}`, i, i, created, i)))
	}
	return out
}

func podTable(t *testing.T, count int) *table {
	t.Helper()
	rendered := newTable(builtin(t, domain.KindPod))
	rendered.replace(pods(t, count), false)
	return rendered
}

// TestFilterFindsAnObjectPastTheFirstWindow is the failure this whole path
// exists to remove: a filter that ran on the rows already sent reported a pod
// that exists as missing, because it had never been sent.
func TestFilterFindsAnObjectPastTheFirstWindow(t *testing.T) {
	rendered := podTable(t, 3000)

	// The oldest pod sorts last in the default order, far beyond any window a
	// table would render at once.
	page := rendered.page(domain.ListQuery{Filter: "api-02999"})

	if page.Matched != 1 {
		t.Fatalf("matched = %d, want the one pod whose name contains the fragment", page.Matched)
	}
	if len(page.Rows) != 1 || page.Rows[0].Name != "api-02999" {
		t.Fatalf("rows = %+v", page.Rows)
	}
	if page.Total != 3000 {
		t.Fatalf("total = %d, want every pod counted whether it matched or not", page.Total)
	}
}

func TestPageReportsTheWholeSetWhileShowingAWindow(t *testing.T) {
	rendered := podTable(t, 1200)

	page := rendered.page(domain.ListQuery{Limit: 500})

	if len(page.Rows) != 500 {
		t.Fatalf("rows = %d, want the window asked for", len(page.Rows))
	}
	if page.Matched != 1200 || page.Total != 1200 {
		t.Fatalf("matched = %d, total = %d, want 1200 for both", page.Matched, page.Total)
	}
	// Newest first, so the first row is the pod created most recently.
	if page.Rows[0].Name != "api-00000" {
		t.Fatalf("first row = %q", page.Rows[0].Name)
	}
}

func TestPagingWalksEveryRowExactlyOnce(t *testing.T) {
	rendered := podTable(t, 1100)

	seen := make(map[string]int)
	for offset := 0; offset < 1100; offset += 500 {
		page := rendered.page(domain.ListQuery{Offset: offset, Limit: 500})
		for _, row := range page.Rows {
			seen[row.Key]++
		}
	}

	if len(seen) != 1100 {
		t.Fatalf("saw %d distinct rows across the pages, want 1100", len(seen))
	}
	for key, times := range seen {
		if times != 1 {
			t.Fatalf("%s appeared %d times", key, times)
		}
	}
}

func TestSortByAColumnOrdersNumbersAsNumbers(t *testing.T) {
	rendered := podTable(t, 12)

	page := rendered.page(domain.ListQuery{SortKey: "restarts", SortDesc: true})

	if page.Rows[0].Fields["restarts"] != "11" {
		t.Fatalf("first restarts = %q, want the highest count", page.Rows[0].Fields["restarts"])
	}
	if page.Rows[1].Fields["restarts"] != "10" {
		t.Fatalf("second restarts = %q, want 10 above 9", page.Rows[1].Fields["restarts"])
	}
}

func TestRowsWithoutTheSortedValueSinkToTheBottom(t *testing.T) {
	rendered := newTable(builtin(t, domain.KindPod))
	rendered.replace(pods(t, 3), false)

	// A pod metrics-server has not answered for has no CPU cell at all.
	rendered.setUsage(map[string]usageRow{
		"shop/api-00001": {cpu: "120m", memory: "64Mi"},
	})

	for _, desc := range []bool{false, true} {
		page := rendered.page(domain.ListQuery{SortKey: "cpu", SortDesc: desc})
		if page.Rows[0].Name != "api-00001" {
			t.Fatalf("descending=%v put %q first, want the only pod with a value", desc, page.Rows[0].Name)
		}
	}
}

func TestUsageFillsThePodColumnsMetricsServerAnswersFor(t *testing.T) {
	rendered := podTable(t, 2)

	touched, _ := rendered.setUsage(map[string]usageRow{
		"shop/api-00000": {cpu: "1.5", memory: "2.1Gi"},
	})

	if len(touched) != 1 || touched[0] != "shop/api-00000" {
		t.Fatalf("touched = %v, want only the pod with usage", touched)
	}

	page := rendered.page(domain.ListQuery{})
	if page.Rows[0].Fields["cpu"] != "1.5" || page.Rows[0].Fields["memory"] != "2.1Gi" {
		t.Fatalf("fields = %+v", page.Rows[0].Fields)
	}
	// The pod without usage keeps empty cells rather than being shown as idle.
	if page.Rows[1].Fields["cpu"] != "" {
		t.Fatalf("a pod with no usage reported %q", page.Rows[1].Fields["cpu"])
	}
}

func TestPatchSendsOnlyTheRowThatChanged(t *testing.T) {
	rendered := podTable(t, 50)
	rendered.page(domain.ListQuery{Limit: 50, Token: "1"})

	// One pod starts crash looping, which changes its cells but not its age,
	// so the order the table is sorted by is untouched.
	crashed := object(t, `{
		"metadata": {"name": "api-00007", "namespace": "shop", "uid": "uid-00007",
			"creationTimestamp": "2025-12-31T23:59:53Z"},
		"spec": {"containers": [{"name": "api"}]},
		"status": {"phase": "Running", "containerStatuses": [
			{"name": "api", "ready": false, "restartCount": 9,
			 "state": {"waiting": {"reason": "CrashLoopBackOff"}}}
		]}
	}`)
	touched, reordered := rendered.apply([]*unstructured.Unstructured{crashed}, nil)
	if reordered {
		t.Fatal("a change to a cell the table is not sorted by forced a re-sort")
	}

	delta, worth := rendered.patch(touched, reordered)
	if !worth {
		t.Fatal("a pod entering CrashLoopBackOff must reach the table")
	}
	if len(delta.Upserts) != 1 || delta.Upserts[0].Name != "api-00007" {
		t.Fatalf("upserts = %+v, want just the crashed pod", delta.Upserts)
	}
	if delta.Order != nil {
		t.Fatalf("order was re-sent for a change that did not reorder anything")
	}
	if delta.Matched != 50 {
		t.Fatalf("matched = %d, want the count to survive a patch that skipped the sort", delta.Matched)
	}
	if delta.Upserts[0].Status != "CrashLoopBackOff" {
		t.Fatalf("status = %q", delta.Upserts[0].Status)
	}
	if delta.Token != "1" {
		t.Fatalf("token = %q, want the query the window was built from", delta.Token)
	}
}

func TestPatchReportsADeletedRowAndTheNewOrder(t *testing.T) {
	rendered := podTable(t, 10)
	rendered.page(domain.ListQuery{Limit: 10})

	touched, reordered := rendered.apply(nil, []string{"shop/api-00003"})
	delta, worth := rendered.patch(touched, reordered)

	if !worth {
		t.Fatal("a deleted pod must reach the table")
	}
	if len(delta.Removed) != 1 || delta.Removed[0] != "shop/api-00003" {
		t.Fatalf("removed = %v", delta.Removed)
	}
	if len(delta.Order) != 9 {
		t.Fatalf("order = %d keys, want the nine that are left", len(delta.Order))
	}
	if delta.Total != 9 || delta.Matched != 9 {
		t.Fatalf("total = %d, matched = %d", delta.Total, delta.Matched)
	}
}

// TestAppendingAWindowKeepsPatchingTheWholeThing covers scrolling: after the
// second window is fetched the frontend holds twenty rows, and a change has to
// be described against all twenty rather than the ten it started with.
func TestAppendingAWindowKeepsPatchingTheWholeThing(t *testing.T) {
	rendered := podTable(t, 40)
	rendered.page(domain.ListQuery{Offset: 0, Limit: 10})
	rendered.page(domain.ListQuery{Offset: 10, Limit: 10})

	touched, reordered := rendered.apply(nil, []string{"shop/api-00015"})
	delta, worth := rendered.patch(touched, reordered)

	if !worth {
		t.Fatal("a pod deleted from the second window must reach the table")
	}
	if len(delta.Order) != 20 {
		t.Fatalf("order = %d keys, want the twenty the frontend holds", len(delta.Order))
	}
	pulled := false
	for _, row := range delta.Upserts {
		if row.Key == "shop/api-00020" {
			pulled = true
		}
	}
	if !pulled {
		t.Fatalf("the row that moved into the second window was not sent: %+v", delta.Upserts)
	}
}

func TestPatchIsSilentWhenTheWindowDidNotMove(t *testing.T) {
	rendered := podTable(t, 20)
	rendered.page(domain.ListQuery{Limit: 20})

	// The same object arriving again — a resync, or an annotation no column
	// shows — must not repaint the table.
	same := pods(t, 20)[4]
	touched, reordered := rendered.apply([]*unstructured.Unstructured{same}, nil)

	if _, worth := rendered.patch(touched, reordered); worth {
		t.Fatal("an unchanged object was reported as a change")
	}
}

func TestPatchFillsTheWindowFromRowsBehindIt(t *testing.T) {
	rendered := podTable(t, 30)
	rendered.page(domain.ListQuery{Limit: 10})

	// Deleting a pod inside the window pulls the eleventh row into it, which
	// the frontend has never been sent and cannot render without.
	touched, reordered := rendered.apply(nil, []string{"shop/api-00002"})
	delta, worth := rendered.patch(touched, reordered)

	if !worth {
		t.Fatal("the window changed and nothing was reported")
	}
	if len(delta.Order) != 10 {
		t.Fatalf("order = %d keys, want a full window", len(delta.Order))
	}
	pulled := false
	for _, row := range delta.Upserts {
		if row.Key == "shop/api-00010" {
			pulled = true
		}
	}
	if !pulled {
		t.Fatalf("the row that scrolled into the window was not sent: %+v", delta.Upserts)
	}
}

// BenchmarkPage measures the work behind one keystroke in the filter box on a
// cluster large enough for it to matter.
func BenchmarkPage(b *testing.B) {
	rendered := newTable(mustBuiltin(domain.KindPod))
	rendered.replace(benchmarkPods(50000), false)

	query := domain.ListQuery{Filter: "api-042", Limit: 500}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rendered.page(query)
	}
}

// BenchmarkPatch measures one debounce window of a rollout: a handful of pods
// changed, against fifty thousand that did not.
func BenchmarkPatch(b *testing.B) {
	rendered := newTable(mustBuiltin(domain.KindPod))
	all := benchmarkPods(50000)
	rendered.replace(all, false)
	rendered.page(domain.ListQuery{Limit: 500})

	changed := all[:20]
	touched := make([]string, 0, len(changed))
	for _, obj := range changed {
		touched = append(touched, RowKey(obj.GetNamespace(), obj.GetName()))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, reordered := rendered.apply(changed, nil)
		rendered.patch(touched, reordered)
	}
}

// BenchmarkPatchReordering measures the same rollout when the change does move
// rows, which is the case that has to sort the whole namespace.
func BenchmarkPatchReordering(b *testing.B) {
	rendered := newTable(mustBuiltin(domain.KindPod))
	all := benchmarkPods(50000)
	rendered.replace(all, false)
	rendered.page(domain.ListQuery{Limit: 500})

	touched := make([]string, 0, 20)
	for _, obj := range all[:20] {
		touched = append(touched, RowKey(obj.GetNamespace(), obj.GetName()))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rendered.patch(touched, true)
	}
}

func mustBuiltin(kind domain.Kind) domain.KindInfo {
	info, ok := domain.Lookup(kind)
	if !ok {
		panic("kind missing from the catalogue: " + string(kind))
	}
	return info
}

func benchmarkPods(count int) []*unstructured.Unstructured {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	out := make([]*unstructured.Unstructured, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, &unstructured.Unstructured{Object: map[string]any{
			"metadata": map[string]any{
				"name":              fmt.Sprintf("api-%05d", i),
				"namespace":         "shop",
				"uid":               fmt.Sprintf("uid-%05d", i),
				"creationTimestamp": base.Add(-time.Duration(i) * time.Second).Format(time.RFC3339),
			},
			"spec": map[string]any{
				"nodeName":   "node-1",
				"containers": []any{map[string]any{"name": "api"}},
			},
			"status": map[string]any{
				"phase": "Running",
				"containerStatuses": []any{map[string]any{
					"name":         "api",
					"ready":        true,
					"restartCount": int64(i % 13),
					"state":        map[string]any{"running": map[string]any{}},
				}},
			},
		}})
	}
	return out
}
