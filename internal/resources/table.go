package resources

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"biebie-kube/internal/domain"
)

// view identifies one table the UI has open: a kind of a cluster, seen through
// one namespace.
type view struct {
	clusterID string
	kind      domain.Kind
	namespace string
}

// table holds every rendered row of one view.
//
// Keeping the rendered rows means a watch notification re-renders the objects
// that changed rather than the whole list, and that a filter or a sort is
// answered without touching Kubernetes at all. The window the frontend holds
// is recorded beside them, so a change can be turned into a patch instead of a
// new table.
type table struct {
	mu sync.Mutex

	info domain.KindInfo

	rows map[string]domain.ResourceRow

	// usage carries the columns that come from somewhere other than the object
	// itself. A pod's CPU and memory are metrics-server's answer, not the
	// pod's, and they arrive on their own schedule.
	usage map[string]usageRow

	// query is the last thing the frontend asked for and sent is the ordered
	// keys it was given, in order. Both are needed to describe a change in
	// terms of what the frontend currently holds.
	query domain.ListQuery
	sent  []string

	// total, matched and reported are the last numbers the frontend was told,
	// so a change that only moves a count still reaches the header.
	total    int
	matched  int
	reported bool

	loading bool

	// scratch backs the filter-and-sort pass, reused between calls.
	scratch []domain.ResourceRow
}

func newTable(info domain.KindInfo) *table {
	return &table{info: info, rows: make(map[string]domain.ResourceRow)}
}

// replace renders a complete set of objects, discarding what was there.
func (t *table) replace(objects []*unstructured.Unstructured, loading bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	rows := make(map[string]domain.ResourceRow, len(objects))
	for _, obj := range objects {
		row := t.renderLocked(obj)
		rows[row.Key] = row
	}
	t.rows = rows
	t.loading = loading
}

// apply re-renders the objects that changed and forgets the ones that are
// gone.
//
// It returns the keys it touched, and whether the order they sit in can still
// stand. Most changes cannot move a row — a restart count ticking up while the
// table is sorted by age — and knowing that is what saves sorting the whole
// cluster three times a second.
func (t *table) apply(changed []*unstructured.Unstructured, removed []string) ([]string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	sorted := t.query.SortKey
	touched := make([]string, 0, len(changed)+len(removed))
	reordered := false

	for _, obj := range changed {
		row := t.renderLocked(obj)
		previous, held := t.rows[row.Key]
		// An informer reports every update, including the ten-minute resync
		// and any edit to a field no column shows. Comparing the rendered row
		// is what keeps those from repainting the table.
		if held && sameRow(previous, row) {
			continue
		}
		if !held || movesInOrder(sorted, previous, row) {
			reordered = true
		}
		t.rows[row.Key] = row
		touched = append(touched, row.Key)
	}
	for _, key := range removed {
		if _, held := t.rows[key]; !held {
			continue
		}
		delete(t.rows, key)
		touched = append(touched, key)
		reordered = true
	}
	return touched, reordered
}

// sameRow reports whether two renders of an object would look identical in the
// table.
func sameRow(left, right domain.ResourceRow) bool {
	if left.UID != right.UID ||
		left.Health != right.Health ||
		left.Status != right.Status ||
		!left.CreatedAt.Equal(right.CreatedAt) ||
		len(left.Fields) != len(right.Fields) {
		return false
	}
	for key, value := range left.Fields {
		if right.Fields[key] != value {
			return false
		}
	}
	return true
}

// setUsage records fresh usage and folds it into the rows already rendered,
// returning the keys whose cells changed and whether the order still stands.
func (t *table) setUsage(usage map[string]usageRow) ([]string, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.usage = usage

	sorted := t.query.SortKey
	touched := make([]string, 0, len(t.rows))
	reordered := false

	for key, row := range t.rows {
		fresh := usage[key]
		if row.Fields["cpu"] == fresh.cpu && row.Fields["memory"] == fresh.memory {
			continue
		}

		// The Fields map is shared with rows already handed out, which are
		// marshalled after this lock is released, so it is copied rather than
		// written through.
		next := row
		next.Fields = fieldsWithUsage(row.Fields, fresh)

		if movesInOrder(sorted, row, next) {
			reordered = true
		}
		t.rows[key] = next
		touched = append(touched, key)
	}
	return touched, reordered
}

// renderLocked renders one object with whatever usage the table holds. The
// caller holds the lock.
func (t *table) renderLocked(obj *unstructured.Unstructured) domain.ResourceRow {
	row := Row(t.info, obj)
	withUsage(&row, t.usage[row.Key])
	return row
}

// withUsage writes the usage columns onto a row just rendered, whose Fields
// map nothing else holds yet.
//
// Absent usage leaves the cells absent rather than zero: an empty cell says
// metrics-server did not answer for this pod, and a zero would say it is idle.
func withUsage(row *domain.ResourceRow, usage usageRow) {
	if usage.cpu == "" && usage.memory == "" {
		return
	}
	if row.Fields == nil {
		row.Fields = make(map[string]string, 2)
	}
	row.Fields["cpu"] = usage.cpu
	row.Fields["memory"] = usage.memory
}

// fieldsWithUsage copies a row's cells with the usage columns replaced.
func fieldsWithUsage(fields map[string]string, usage usageRow) map[string]string {
	next := make(map[string]string, len(fields)+2)
	for key, value := range fields {
		next[key] = value
	}
	for column, value := range map[string]string{"cpu": usage.cpu, "memory": usage.memory} {
		if value == "" {
			delete(next, column)
			continue
		}
		next[column] = value
	}
	return next
}

// page answers a query and records the window it handed out.
//
// An offset of zero replaces what the frontend holds; anything else appends to
// it, which is how the table grows as the user scrolls.
func (t *table) page(query domain.ListQuery) domain.ResourcePage {
	query = query.Normalise()

	t.mu.Lock()
	defer t.mu.Unlock()

	t.query = query

	ordered := t.orderedLocked(query)
	window := windowOf(ordered, query.Offset, query.Limit)

	// The frontend holds everything up to the end of this window, so the whole
	// prefix is recorded rather than appended to. The order below an append can
	// have shifted since it was sent, and recording what is true now is what
	// lets the next patch re-send exactly the rows that moved.
	held := query.Offset + len(window)
	if held > len(ordered) {
		held = len(ordered)
	}
	t.sent = keysOf(ordered[:held])

	t.total = len(t.rows)
	t.matched = len(ordered)
	t.reported = t.loading

	return domain.ResourcePage{
		Kind:       t.info.Kind,
		Columns:    t.info.Columns,
		Namespaced: t.info.Namespaced,
		Rows:       window,
		Total:      t.total,
		Matched:    t.matched,
		Offset:     query.Offset,
		Loading:    t.loading,
	}
}

// rowsDelta is what changed about the window the frontend holds.
type rowsDelta struct {
	// Upserts are rows that are new to the window or whose contents changed.
	Upserts []domain.ResourceRow `json:"upserts,omitempty"`

	// Removed are keys that have left the window, whether deleted from the
	// cluster or pushed past its end.
	Removed []string `json:"removed,omitempty"`

	// Order is the window's keys in order, sent only when the order changed.
	// A rollout usually only changes rows in place, and re-sending the order
	// every time would be most of the payload it saves.
	Order []string `json:"order,omitempty"`

	Total   int  `json:"total"`
	Matched int  `json:"matched"`
	Loading bool `json:"loading"`

	// Token is the query this window was built from.
	Token string `json:"token"`
}

// patch describes the table's current state as a change to the window the
// frontend was last given, and reports whether there is anything to say.
//
// touched names the objects re-rendered since the last patch. A row in the
// window is sent when it was touched or when it was not in the window before;
// everything else the frontend already has, correct, and is left alone.
func (t *table) patch(touched []string, reordered bool) (rowsDelta, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !reordered {
		return t.inPlaceLocked(touched)
	}

	ordered := t.orderedLocked(t.query)

	// The frontend holds a prefix of the ordered list, so the window keeps its
	// size unless there are no longer enough rows to fill it.
	size := len(t.sent)
	if size > len(ordered) {
		size = len(ordered)
	}
	window := ordered[:size]

	previous := make(map[string]struct{}, len(t.sent))
	for _, key := range t.sent {
		previous[key] = struct{}{}
	}
	changed := make(map[string]struct{}, len(touched))
	for _, key := range touched {
		changed[key] = struct{}{}
	}

	delta := rowsDelta{
		Total:   len(t.rows),
		Matched: len(ordered),
		Loading: t.loading,
		Token:   t.query.Token,
	}

	order := keysOf(window)
	present := make(map[string]struct{}, len(order))
	for _, key := range order {
		present[key] = struct{}{}
	}

	for _, row := range window {
		_, held := previous[row.Key]
		if _, dirty := changed[row.Key]; dirty || !held {
			delta.Upserts = append(delta.Upserts, row)
		}
	}
	for _, key := range t.sent {
		if _, still := present[key]; !still {
			delta.Removed = append(delta.Removed, key)
		}
	}
	if !sameOrder(t.sent, order) {
		delta.Order = order
	}

	changedCounts := delta.Total != t.total || delta.Matched != t.matched || delta.Loading != t.reported
	t.total, t.matched, t.reported = delta.Total, delta.Matched, delta.Loading
	t.sent = order

	// A change to a field no column shows leaves the window identical.
	// Reporting it anyway would repaint the table for nothing, which is the
	// cost this whole path exists to remove.
	quiet := len(delta.Upserts) == 0 && len(delta.Removed) == 0 && delta.Order == nil
	return delta, !quiet || changedCounts
}

// inPlaceLocked describes changes that cannot have moved a row.
//
// Nothing was added, nothing was removed and no sorted value changed, so the
// window the frontend holds is still the right window and still in the right
// order: only some of its cells are different. Not sorting is the whole point
// of knowing that, because sorting is the expensive part.
func (t *table) inPlaceLocked(touched []string) (rowsDelta, bool) {
	delta := rowsDelta{
		Total:   len(t.rows),
		Matched: t.matched,
		Loading: t.loading,
		Token:   t.query.Token,
	}

	held := make(map[string]struct{}, len(t.sent))
	for _, key := range t.sent {
		held[key] = struct{}{}
	}
	for _, key := range touched {
		if _, inWindow := held[key]; !inWindow {
			continue
		}
		if row, ok := t.rows[key]; ok {
			delta.Upserts = append(delta.Upserts, row)
		}
	}

	changedCounts := delta.Total != t.total || delta.Loading != t.reported
	t.total, t.reported = delta.Total, delta.Loading

	return delta, len(delta.Upserts) > 0 || changedCounts
}

// orderedLocked filters and sorts every row. The caller holds the lock.
//
// The result borrows the table's scratch space and must not outlive the lock.
// Allocating a slice the size of the cluster on every keystroke and every
// reordering change is most of what a large namespace costs.
func (t *table) orderedLocked(query domain.ListQuery) []domain.ResourceRow {
	needle := strings.ToLower(strings.TrimSpace(query.Filter))

	rows := t.scratch[:0]
	for _, row := range t.rows {
		if needle != "" && !strings.Contains(strings.ToLower(row.Name), needle) {
			continue
		}
		rows = append(rows, row)
	}
	t.scratch = rows

	sortRows(rows, query.Normalise())
	return rows
}

// sortRows orders a filtered set.
//
// The sorted cell is read once per row rather than once per comparison. A
// namespace of fifty thousand pods is a couple of million comparisons, and
// reading — worse, formatting — a value inside each one is the difference
// between a sort that is imperceptible and one that stalls the table.
//
// Rows with nothing in the sorted column sink to the bottom whichever way the
// column is sorted: a screen of dashes above the data is not a sort, and it is
// what an engineer sees when they click a column half the rows do not fill.
func sortRows(rows []domain.ResourceRow, query domain.ListQuery) {
	key, desc := query.SortKey, query.SortDesc

	keyed := make([]keyedRow, len(rows))
	for i, row := range rows {
		keyed[i] = keyRow(row, key, i)
	}

	// An unstable sort is safe here, and much faster: every row ends with a
	// comparison against its unique key, so the order is total and the result
	// is the same every time.
	slices.SortFunc(keyed, func(first, second keyedRow) int {
		if first.empty != second.empty {
			if first.empty {
				return 1
			}
			return -1
		}

		left, right := first, second
		if desc {
			left, right = second, first
		}
		if left.numeric && right.numeric {
			if order := cmp.Compare(left.number, right.number); order != 0 {
				return order
			}
		} else if order := strings.Compare(left.text, right.text); order != 0 {
			return order
		}
		// Name is the tiebreak everywhere and always ascending, so the pods of
		// one rollout — all created within the same second — read
		// alphabetically whichever way the sorted column points.
		return strings.Compare(first.key, second.key)
	})

	permute(rows, keyed)
}

// keyedRow is one row's sorted cell, read once.
//
// A leading number is kept beside the text because a restart count of 10 must
// not sort below 2, and a "3/5" ready column must order by the three. Sorting
// these rather than the rows themselves matters: a row is several times the
// size, and a namespace of fifty thousand is a couple of million swaps.
type keyedRow struct {
	key     string
	text    string
	number  float64
	numeric bool
	empty   bool

	// at is where the row started, so the rows can be permuted to match.
	at int
}

func keyRow(row domain.ResourceRow, key string, at int) keyedRow {
	if key == domain.SortKeyCreated || key == "" {
		// Milliseconds rather than nanoseconds: a float64 cannot hold Unix
		// nanoseconds exactly, and Kubernetes records creation to the second
		// anyway.
		return keyedRow{
			key:     row.Key,
			number:  float64(row.CreatedAt.UnixMilli()),
			numeric: true,
			empty:   row.CreatedAt.IsZero(),
			at:      at,
		}
	}

	value := sortValue(row, key)
	number, numeric := leadingNumber(value)
	return keyedRow{
		key:     row.Key,
		text:    strings.ToLower(value),
		number:  number,
		numeric: numeric,
		empty:   value == "",
		at:      at,
	}
}

func permute(rows []domain.ResourceRow, keyed []keyedRow) {
	sorted := make([]domain.ResourceRow, len(rows))
	for i, entry := range keyed {
		sorted[i] = rows[entry.at]
	}
	copy(rows, sorted)
}

// sortValue reads the column a sort names.
//
// Status is asked for by name often enough to be worth resolving to the row's
// own status first, since that is where the renderers put the word an engineer
// is sorting for.
func sortValue(row domain.ResourceRow, key string) string {
	switch key {
	case domain.SortKeyName:
		return row.Name
	case domain.SortKeyNamespace:
		return row.Namespace
	case domain.SortKeyStatus:
		if row.Status != "" {
			return row.Status
		}
	}
	return row.Fields[key]
}

// movesInOrder reports whether a re-render changed the value the table is
// sorted by, and so could have moved the row.
func movesInOrder(key string, before, after domain.ResourceRow) bool {
	if key == domain.SortKeyCreated || key == "" {
		return !before.CreatedAt.Equal(after.CreatedAt)
	}
	return sortValue(before, key) != sortValue(after, key)
}

func leadingNumber(value string) (float64, bool) {
	end := 0
	for end < len(value) {
		c := value[end]
		if (c >= '0' && c <= '9') || (c == '.' && end > 0) || (c == '-' && end == 0) {
			end++
			continue
		}
		break
	}
	if end == 0 {
		return 0, false
	}
	number, err := strconv.ParseFloat(value[:end], 64)
	if err != nil {
		return 0, false
	}
	return number, true
}

func windowOf(rows []domain.ResourceRow, offset, limit int) []domain.ResourceRow {
	if offset >= len(rows) {
		return []domain.ResourceRow{}
	}
	end := offset + limit
	if end > len(rows) {
		end = len(rows)
	}
	out := make([]domain.ResourceRow, end-offset)
	copy(out, rows[offset:end])
	return out
}

func keysOf(rows []domain.ResourceRow) []string {
	keys := make([]string, len(rows))
	for i, row := range rows {
		keys[i] = row.Key
	}
	return keys
}

func sameOrder(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
