package memstore_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"strings"
	"testing"

	"memstore"

	_ "modernc.org/sqlite"
)

func openTemp(t *testing.T) *memstore.Store {
	t.Helper()
	s, err := memstore.Open(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// Spec acceptance 1: GIVEN an empty store WHEN put a memory THEN get returns
// it byte-identical.
func TestPutGetRoundTrip(t *testing.T) {
	s := openTemp(t)
	in := memstore.Memory{
		Kind:       "note",
		Scope:      "memstore",
		Title:      "Byte round trip",
		Text:       "line1\nline2 \"quoted\" — ünïcode 🧠\ttab",
		Extra:      `{"src":"test"}`,
		Importance: 0.5,
		FoundBy:    "memstore:t-925378d6",
	}
	id, err := s.Put(&in)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if id <= 0 {
		t.Fatalf("Put id = %d, want > 0", id)
	}
	got, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Text != in.Text {
		t.Errorf("Text not byte-identical:\n got %q\nwant %q", got.Text, in.Text)
	}
	if got.Id != id || got.Kind != in.Kind || got.Scope != in.Scope ||
		got.Title != in.Title || got.Extra != in.Extra ||
		got.Importance != in.Importance || got.FoundBy != in.FoundBy {
		t.Errorf("fields diverged:\n got %+v\nfrom %+v", got, in)
	}
	if got.Status != "open" {
		t.Errorf("Status = %q, want %q", got.Status, "open")
	}
	if got.Ts.IsZero() {
		t.Error("Ts not set by Put")
	}
	sum := sha256.Sum256([]byte(in.Text))
	if got.Hash != hex.EncodeToString(sum[:]) {
		t.Errorf("Hash = %q, want sha256(text) %q", got.Hash, hex.EncodeToString(sum[:]))
	}
	if got.DeletedAt != nil {
		t.Errorf("DeletedAt = %v, want nil", got.DeletedAt)
	}
}

// Spec acceptance 6: a put with empty text is rejected.
func TestPutEmptyTextRefused(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Put(&memstore.Memory{Kind: "note"}); err == nil {
		t.Error("Put with empty text: got nil error, want refusal")
	}
}

// Spec acceptance 6: the smallest accepted input is non-empty text + kind.
func TestPutEmptyKindRefused(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Put(&memstore.Memory{Text: "some text"}); err == nil {
		t.Error("Put with empty kind: got nil error, want refusal")
	}
}

// Spec data requirements: unknown kind accepted (open label).
func TestUnknownKindAccepted(t *testing.T) {
	s := openTemp(t)
	id, err := s.Put(&memstore.Memory{Kind: "a-kind-nobody-named", Text: "stored, not refused"})
	if err != nil {
		t.Fatalf("Put with unknown kind: %v", err)
	}
	got, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Kind != "a-kind-nobody-named" {
		t.Errorf("Kind = %q, want %q", got.Kind, "a-kind-nobody-named")
	}
}

// Card acceptance 4: FTS5 external content — the text is indexed in place,
// so no record_fts_content copy table may exist.
func TestFTSExternalContentNoCopy(t *testing.T) {
	s := openTemp(t)
	payload := "x7kq9-zw3vm-payload-" + strings.Repeat("no-second-copy ", 20)
	if _, err := s.Put(&memstore.Memory{Kind: "note", Text: payload}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	db, err := sql.Open("sqlite", s.Path())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	var n int
	err = db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'record_fts_content'`).Scan(&n)
	if err != nil {
		t.Fatalf("query sqlite_master: %v", err)
	}
	if n != 0 {
		t.Error("record_fts_content exists — the text is being copied, not indexed in place")
	}
	// And the payload must still be searchable: indexed from memory itself.
	hits, err := s.Search("x7kq9-zw3vm-payload", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || hits[0].Text != payload {
		t.Errorf("Search for unique payload: got %d hits, want the 1 memory", len(hits))
	}
}

// Card acceptance 5: a query whose words occur in a memory returns it; a
// query with no match returns empty, not an error.
func TestSearchFindsMemory(t *testing.T) {
	s := openTemp(t)
	a, err := s.Put(&memstore.Memory{Kind: "note", Title: "Deploy runbook", Text: "restart the webhook relay when the queue backs up"})
	if err != nil {
		t.Fatalf("Put A: %v", err)
	}
	if _, err := s.Put(&memstore.Memory{Kind: "note", Title: "Coffee", Text: "oat milk flat white, no sugar"}); err != nil {
		t.Fatalf("Put B: %v", err)
	}
	c, err := s.Put(&memstore.Memory{Kind: "note", Title: "Meeting notes", Text: "webhook relay latency and retries discussed"})
	if err != nil {
		t.Fatalf("Put C: %v", err)
	}

	hits, err := s.Search("webhook relay", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	found := map[int64]bool{}
	for _, h := range hits {
		found[h.Id] = true
	}
	if !found[a] || !found[c] {
		t.Errorf("Search(webhook relay) found %v, want ids %d and %d", found, a, c)
	}

	hits, err = s.Search("term-that-occurs-nowhere", 5)
	if err != nil {
		t.Fatalf("Search with no match must not error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search with no match: got %d hits, want 0", len(hits))
	}
}

// Card acceptance 6: content is immutable — a second Put of the same id is
// refused rather than overwriting.
func TestSecondPutSameIdRefused(t *testing.T) {
	s := openTemp(t)
	id, err := s.Put(&memstore.Memory{Kind: "note", Text: "original content"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	_, err = s.Put(&memstore.Memory{Id: id, Kind: "note", Text: "overwriting content"})
	if err == nil {
		t.Error("second Put with same id: got nil error, want refusal")
	}
	got, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Text != "original content" {
		t.Errorf("Text = %q after refused Put, want %q", got.Text, "original content")
	}
}

// Card section 1: a schema_version row exists.
func TestSchemaVersion(t *testing.T) {
	s := openTemp(t)
	db, err := sql.Open("sqlite", s.Path())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	var v int
	err = db.QueryRow(`SELECT version FROM schema_version`).Scan(&v)
	if err != nil {
		t.Fatalf("SELECT schema_version: %v", err)
	}
	if v != 1 {
		t.Errorf("schema_version = %d, want 1", v)
	}
}

// Get of an id that was never put is an error, not a zero memory.
func TestGetMissingId(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Get(424242); err == nil {
		t.Error("Get(missing id): got nil error, want error")
	}
}
