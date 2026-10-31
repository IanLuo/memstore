package memstore_test

import (
	"testing"

	"github.com/IanLuo/memstore"
)

func queryFixture(t *testing.T) *memstore.Store {
	t.Helper()
	s := openTemp(t)
	for _, m := range []memstore.Memory{
		{Kind: "decision", Scope: "alpha", Title: "Queue drains on restart", Text: "the queue drains when the worker restarts"},
		{Kind: "knowledge", Scope: "alpha", Title: "Worker start", Text: "worker start clears the queue"},
		{Kind: "note", Scope: "beta", Title: "Scratch", Text: "unrelated note about lunch"},
	} {
		if _, err := s.Put(&m); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	return s
}

// The web view must use only the public reads agents already have. Search
// preserves the matcher's ranking; Recent lists the chosen scope newest first;
// Get resolves one row; Stats reports the store shape.
func TestPublicReadOperations(t *testing.T) {
	s := queryFixture(t)
	hits, err := s.Search("queue", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("Search(queue) = %d hits, want 2", len(hits))
	}
	recent, err := s.Recent("alpha", 10)
	if err != nil {
		t.Fatalf("Recent(alpha): %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("Recent(alpha) = %d rows, want 2", len(recent))
	}
	got, err := s.Get(recent[0].Id)
	if err != nil {
		t.Fatalf("Get(%d): %v", recent[0].Id, err)
	}
	if got.Id != recent[0].Id {
		t.Fatalf("Get id = %d, want %d", got.Id, recent[0].Id)
	}
	stats, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Total != 3 || stats.Open != 3 {
		t.Fatalf("Stats = %+v, want 3 live rows", stats)
	}
}
