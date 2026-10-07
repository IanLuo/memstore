package memstore_test

import (
	"os"
	"strings"
	"testing"

	"github.com/IanLuo/memstore"
)

// Card acceptance 1-4: with no embedder configured, the lexical matcher
// answers a paraphrase — bm25 over the porter-stemmed index, OR across the
// query's stems — and returns nothing only when no stem is shared. The store
// here never calls SetEmbedder, so the vector matcher is absent and every hit
// is lexical.
func TestSearchAnswersParaphrase(t *testing.T) {
	s := openTemp(t)
	const row = "memstore replaces the fs store layer: one SQLite file, FTS external-content, no mirror table"
	target, err := s.Put(&memstore.Memory{Kind: "decision", Text: row})
	if err != nil {
		t.Fatalf("Put target: %v", err)
	}
	gate, err := s.Put(&memstore.Memory{Kind: "note", Text: "the release gates block the merge"})
	if err != nil {
		t.Fatalf("Put gate: %v", err)
	}

	// 1. One absent token must not sink a query whose other tokens match.
	hits, err := s.Search("one file hold", 5)
	if err != nil {
		t.Fatalf("Search(one file hold): %v", err)
	}
	if !containsID(hits, target) {
		t.Errorf("Search(one file hold) = %v, want %d in the top 5 — 'hold' is absent and the rest must still match", ids(hits), target)
	}

	// 2. A query reaches the stored word's inflection: 'gate' is the stem of 'gates'.
	hits, err = s.Search("gate", 5)
	if err != nil {
		t.Fatalf("Search(gate): %v", err)
	}
	if !containsID(hits, gate) {
		t.Errorf("Search(gate) = %v, want %d — the index must stem 'gates'", ids(hits), gate)
	}

	// 3. The memory's own text with one word removed ranks it first.
	hits, err = s.Search(strings.Replace(row, "mirror ", "", 1), 5)
	if err != nil {
		t.Fatalf("Search(own text minus a word): %v", err)
	}
	if len(hits) == 0 || hits[0].Id != target {
		t.Errorf("Search(own text minus a word) = %v, want the target %d first", ids(hits), target)
	}

	// 4. The fail-open half: no shared stem, no embedder, nothing — never noise.
	hits, err = s.Search("how is memory persisted locally", 5)
	if err != nil {
		t.Fatalf("Search(zero overlap): %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search(zero overlap) = %v, want no rows with no shared stem and no embedder", ids(hits))
	}
}

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

// Card acceptance 2 (as amended): matchers.go performs no writes. Its only
// handle is a query handle — write-capable, which is why the claim is pinned
// by its source, and why the wording says performs, not cannot.
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
