package memstore_test

import (
	"os"
	"strings"
	"testing"

	"memstore"
)

// Card section 2 / slice-4 review A1: a query with no alphanumeric token has
// no lexical expression, and that used to short-circuit Search before the
// vector matcher. With an embedder configured the query must still embed and
// the vector matcher must still run; with no embedder the result stays empty.
func TestSearchTokenlessConsultsVector(t *testing.T) {
	const q = "!!!"

	// With an embedder: count the calls and return one fixed non-zero vector,
	// so every row shares the query's direction (cosine 1) and a hit is
	// reachable. Otherwise the result would be empty for a reason other than
	// the short-circuit.
	on := openTemp(t)
	calls := 0
	if err := on.SetEmbedder(func(string) ([]float32, error) {
		calls++
		return []float32{1, 0, 0, 0}, nil
	}); err != nil {
		t.Fatalf("SetEmbedder: %v", err)
	}
	id, err := on.Put(&memstore.Memory{Kind: "note", Text: "anything at all"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	calls = 0 // Put embeds too; count only the search.
	hits, err := on.Search(q, 5)
	if err != nil {
		t.Fatalf("Search (tokenless, embedder): %v", err)
	}
	if calls == 0 {
		t.Error("tokenless query never reached the embedder — the vector matcher was short-circuited")
	}
	if !containsID(hits, id) {
		t.Errorf("Search(tokenless) = %v, want the row %d found by the vector matcher", ids(hits), id)
	}

	// With no embedder there is no vector matcher, so a tokenless query is
	// empty — the degrade rule, unchanged.
	off := openTemp(t)
	if _, err := off.Put(&memstore.Memory{Kind: "note", Text: "anything at all"}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	hits, err = off.Search(q, 5)
	if err != nil {
		t.Fatalf("Search (tokenless, no embedder): %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search(tokenless) without an embedder = %v, want empty", ids(hits))
	}
}

// Card acceptance 2: the bm25, cosine and RRF logic lives in Matchers. Store
// is persistence and lifecycle and must not carry it. The pin is over the
// source because the boundary is where the code lives, not what one call
// returns.
func TestMatchersOwnsRanking(t *testing.T) {
	storeSrc := readSource(t, "store.go")
	for _, gone := range []string{"bm25(", "cosine(", "fuse("} {
		if strings.Contains(storeSrc, gone) {
			t.Errorf("store.go contains %q — ranking belongs in Matchers", gone)
		}
	}
	matchSrc := readSource(t, "matchers.go")
	for _, want := range []string{"bm25(", "cosine(", "fuse("} {
		if !strings.Contains(matchSrc, want) {
			t.Errorf("matchers.go does not contain %q", want)
		}
	}
}

// Card acceptance 2: Matchers holds no write path. Its only handle is the
// read interface, so its source must name no write statement or write call.
func TestMatchersHasNoWritePath(t *testing.T) {
	src := readSource(t, "matchers.go")
	for _, banned := range []string{"Exec(", "CREATE TABLE", "INSERT INTO", "UPDATE ", "DELETE FROM"} {
		if strings.Contains(src, banned) {
			t.Errorf("matchers.go contains %q — Matchers must hold no write path", banned)
		}
	}
}

func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}
