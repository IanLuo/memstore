package memstore_test

import (
	"testing"

	"github.com/IanLuo/memstore"
)

// put is the fixture every query test starts from: six live rows across two
// kinds, two scopes and two origins, plus one row forgotten after the put.
func queryFixture(t *testing.T) *memstore.Store {
	t.Helper()
	s := openTemp(t)
	rows := []memstore.Memory{
		{Kind: "decision", Scope: "alpha", Title: "Queue drains on restart", Text: "the queue drains when the worker restarts", Origin: "native"},
		{Kind: "decision", Scope: "alpha", Title: "Retry budget", Text: "the queue retry budget is three attempts", Origin: "native"},
		{Kind: "knowledge", Scope: "alpha", Title: "Worker start", Text: "worker start clears the queue", Origin: "native"},
		{Kind: "knowledge", Scope: "beta", Title: "Imported queue note", Text: "queue depth is reported by the source event", Origin: "imported", OriginId: "fs-1"},
		{Kind: "note", Scope: "beta", Title: "Scratch", Text: "unrelated note about lunch", Origin: "native"},
		{Kind: "note", Scope: "beta", Title: "Closed note", Text: "queue note that was replaced", Origin: "native", Status: "open"},
	}
	for i := range rows {
		if _, err := s.Put(&rows[i]); err != nil {
			t.Fatalf("Put %d: %v", i, err)
		}
	}
	gone, err := s.Put(&memstore.Memory{Kind: "decision", Scope: "alpha", Title: "Forgotten", Text: "queue row that is forgotten", Origin: "native"})
	if err != nil {
		t.Fatalf("Put forgotten: %v", err)
	}
	if err := s.Forget(gone); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	return s
}

// Spec: exact filters combine with AND, and a returned row always satisfies
// every filter it was asked for.
func TestQueryFiltersCombineWithAnd(t *testing.T) {
	s := queryFixture(t)
	hits, _, err := s.Query("", memstore.Filter{Scope: "beta", Origin: "imported"}, 20, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) != 1 || hits[0].Title != "Imported queue note" {
		t.Fatalf("beta+imported = %+v, want the imported beta row only", hits)
	}
	hits, _, err = s.Query("queue", memstore.Filter{Kind: "knowledge", Scope: "beta"}, 20, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) != 1 || hits[0].Kind != "knowledge" || hits[0].Scope != "beta" {
		t.Fatalf("queue+knowledge+beta = %+v, want one matching row", hits)
	}
}

// Spec: filtering happens before the limit. A row that matches the filter but
// ranks below an unfiltered page boundary must still come back inside the
// filtered page — post-filtering a truncated top-k would return nothing here.
func TestQueryFiltersBeforeLimit(t *testing.T) {
	s := queryFixture(t)
	first, _, err := s.Query("queue", memstore.Filter{}, 1, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(first) != 1 {
		t.Fatalf("unfiltered page = %d rows, want 1", len(first))
	}
	if first[0].Kind == "note" {
		t.Skip("fixture ranking put the note first; the check below needs a different head")
	}
	hit, more, err := s.Query("queue", memstore.Filter{Kind: "note"}, 1, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hit) != 1 || hit[0].Kind != "note" || hit[0].Title != "Closed note" {
		t.Fatalf("kind:note page = %+v, want the note row even though it is not the overall head", hit)
	}
	if more {
		t.Errorf("hasMore = true with one of one note rows")
	}
}

// Spec: text results keep the store's ranking among the rows that survive the
// filter, so the order is a subsequence of the unfiltered order.
func TestQueryTextOrderPreservedUnderFilter(t *testing.T) {
	s := queryFixture(t)
	all, _, err := s.Query("queue", memstore.Filter{}, 20, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	filtered, _, err := s.Query("queue", memstore.Filter{Scope: "alpha"}, 20, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	var want []int64
	for _, m := range all {
		if m.Scope == "alpha" {
			want = append(want, m.Id)
		}
	}
	var got []int64
	for _, m := range filtered {
		got = append(got, m.Id)
	}
	if len(got) != len(want) {
		t.Fatalf("filtered %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("filtered order %v, want %v", got, want)
		}
	}
}

// Spec: a filter-only request lists newest first and skips forgotten rows.
func TestQueryFilterOnlyNewestFirst(t *testing.T) {
	s := queryFixture(t)
	hits, _, err := s.Query("", memstore.Filter{Scope: "alpha"}, 20, 0)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(hits) != 3 {
		t.Fatalf("alpha rows = %d, want 3 (the forgotten row is hidden)", len(hits))
	}
	for i := 1; i < len(hits); i++ {
		if hits[i].Ts.After(hits[i-1].Ts) {
			t.Fatalf("row %d is newer than row %d: not newest-first", i, i-1)
		}
	}
	for _, m := range hits {
		if m.DeletedAt != nil {
			t.Fatalf("forgotten row %d was returned", m.Id)
		}
	}
}

// Spec: pages are deterministic and non-overlapping, and hasMore answers
// without a count query.
func TestQueryPagesAreStableAndDisjoint(t *testing.T) {
	s := queryFixture(t)
	seen := map[int64]bool{}
	for offset := 0; ; offset += 2 {
		hits, more, err := s.Query("", memstore.Filter{}, 2, offset)
		if err != nil {
			t.Fatalf("Query offset %d: %v", offset, err)
		}
		for _, m := range hits {
			if seen[m.Id] {
				t.Fatalf("memory %d returned twice", m.Id)
			}
			seen[m.Id] = true
		}
		if !more {
			break
		}
		if offset > 20 {
			t.Fatal("hasMore never cleared")
		}
	}
	if len(seen) != 6 {
		t.Fatalf("paged over %d rows, want 6 live rows", len(seen))
	}
}

// Spec: a limit outside 1..MaxPageSize and a negative offset are refused, not
// clamped, so a caller cannot mistake a short page for the end of the store.
func TestQueryRefusesBadPage(t *testing.T) {
	s := queryFixture(t)
	for _, tc := range []struct {
		name          string
		limit, offset int
	}{
		{"zero limit", 0, 0},
		{"limit over max", memstore.MaxPageSize + 1, 0},
		{"negative offset", 10, -1},
	} {
		if _, _, err := s.Query("", memstore.Filter{}, tc.limit, tc.offset); err == nil {
			t.Errorf("%s: got nil error, want refusal", tc.name)
		}
	}
	if _, _, err := s.Query("", memstore.Filter{}, memstore.MaxPageSize, 0); err != nil {
		t.Errorf("limit MaxPageSize refused: %v", err)
	}
}

// Spec: Relations returns the stored edges of a live memory and reports an
// edge whose target was forgotten by id, without inventing its content.
func TestRelationsReportsHiddenTarget(t *testing.T) {
	s := openTemp(t)
	from, err := s.Put(&memstore.Memory{Kind: "decision", Text: "old decision", Title: "Old"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	to, err := s.Put(&memstore.Memory{Kind: "decision", Text: "new decision", Title: "New"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.CloseMemory(from, &to); err != nil {
		t.Fatalf("CloseMemory: %v", err)
	}
	edges, err := s.Relations(from)
	if err != nil {
		t.Fatalf("Relations: %v", err)
	}
	if len(edges) != 1 || edges[0].Rel != "superseded-by" || edges[0].To != to {
		t.Fatalf("Relations = %+v, want superseded-by %d", edges, to)
	}
	if err := s.Forget(to); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	edges, err = s.Relations(from)
	if err != nil {
		t.Fatalf("Relations after forget: %v", err)
	}
	if len(edges) != 1 || edges[0].To != to {
		t.Fatalf("Relations after forget = %+v, want the edge by id", edges)
	}
	if _, err := s.Get(to); err == nil {
		t.Error("Get on the forgotten target: got nil error, want refusal")
	}
	if _, err := s.Relations(to); err == nil {
		t.Error("Relations on a forgotten memory: got nil error, want refusal")
	}
}
