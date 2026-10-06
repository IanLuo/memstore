package memstore_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

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

// Card section 1 / A1: a fresh store records schema_version 2 — the
// unix-nanosecond timestamp format — and opens.
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
	if v != 2 {
		t.Errorf("schema_version = %d, want 2", v)
	}
}

// Card A1: a database stamped with a schema_version Open does not expect is
// refused on Open, naming the version found — never silently misread as if it
// were the current format. Each stamp gets its own fresh file so a refused
// file never backs a later stage.
func TestOpenRefusesUnexpectedSchemaVersion(t *testing.T) {
	for _, stamp := range []int{1, 3} {
		path := filepath.Join(t.TempDir(), "memory.db")
		// A fresh store records the current version; stamp the row over.
		s, err := memstore.Open(path)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		s.Close()
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatalf("sql.Open: %v", err)
		}
		if _, err := db.Exec(`UPDATE schema_version SET version = ?`, stamp); err != nil {
			t.Fatalf("stamp %d: %v", stamp, err)
		}
		db.Close()

		if _, err := memstore.Open(path); err == nil {
			t.Errorf("Open of schema_version-%d db: got nil error, want refusal", stamp)
		} else if got := err.Error(); !strings.Contains(got, fmt.Sprintf("schema_version is %d", stamp)) {
			t.Errorf("Open of schema_version-%d db refused with %q, want the found version %d named", stamp, got, stamp)
		}
	}
}

// Get of an id that was never put is an error, not a zero memory.
func TestGetMissingId(t *testing.T) {
	s := openTemp(t)
	if _, err := s.Get(424242); err == nil {
		t.Error("Get(missing id): got nil error, want error")
	}
}

// Card F1: slice 1 has no forget verb, so a caller-supplied DeletedAt is
// refused — deletion state gets one writer, the future forget verb.
func TestPutRefusesDeletedAt(t *testing.T) {
	s := openTemp(t)
	now := time.Now()
	_, err := s.Put(&memstore.Memory{Kind: "note", Text: "already forgotten", DeletedAt: &now})
	if err == nil {
		t.Error("Put with DeletedAt set: got nil error, want refusal")
	}
}

// Card F1 / spec acceptance 4: a soft-deleted row is hidden from reads. Slice 1
// has no forget verb, so the state is set directly, as forget will set it.
func TestSoftDeletedHiddenFromReads(t *testing.T) {
	s := openTemp(t)
	id, err := s.Put(&memstore.Memory{Kind: "note", Text: "soft-deleted-quokka-marker"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	db, err := sql.Open("sqlite", s.Path())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE memory SET deleted_at = ? WHERE id = ?`, time.Now().UnixNano(), id); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}

	if _, err := s.Get(id); err == nil {
		t.Error("Get of a soft-deleted row: got nil error, want it hidden")
	}
	hits, err := s.Search("soft-deleted-quokka-marker", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search found %d soft-deleted rows, want 0", len(hits))
	}
}

// Card F1 / spec acceptance 2: only deletion hides. A closed or superseded row
// is still returned, and a superseded row carries its successor's id.
func TestSearchKeepsClosedAndSuperseded(t *testing.T) {
	s := openTemp(t)
	succ, err := s.Put(&memstore.Memory{Kind: "decision", Text: "the successor sünnset plan"})
	if err != nil {
		t.Fatalf("Put successor: %v", err)
	}
	sup := succ
	d1, err := s.Put(&memstore.Memory{Kind: "decision", Text: "oldquokka plan", Status: "closed", Supersedes: &sup})
	if err != nil {
		t.Fatalf("Put superseded: %v", err)
	}
	closedOnly, err := s.Put(&memstore.Memory{Kind: "note", Text: "clozzured note", Status: "closed"})
	if err != nil {
		t.Fatalf("Put closed: %v", err)
	}

	hits, err := s.Search("oldquokka", 5)
	if err != nil {
		t.Fatalf("Search superseded: %v", err)
	}
	if len(hits) != 1 || hits[0].Id != d1 {
		t.Fatalf("Search found %d hits (%v), want the superseded row %d", len(hits), hits, d1)
	}
	if hits[0].Supersedes == nil || *hits[0].Supersedes != succ {
		t.Errorf("superseded row Supersedes = %v, want %d", hits[0].Supersedes, succ)
	}

	hits, err = s.Search("clozzured", 5)
	if err != nil {
		t.Fatalf("Search closed: %v", err)
	}
	if len(hits) != 1 || hits[0].Id != closedOnly {
		t.Errorf("Search found %d hits (%v), want the closed row %d", len(hits), hits, closedOnly)
	}
}

// Card F3: Ts survives Put→Get with sub-second precision and a non-UTC zone,
// so the instant compares Equal.
func TestTsRoundTrip(t *testing.T) {
	s := openTemp(t)
	ts := time.Date(2026, 10, 5, 11, 36, 15, 123456789, time.FixedZone("UTC+8", 8*3600))
	id, err := s.Put(&memstore.Memory{Kind: "note", Text: "timestamped", Ts: ts})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, err := s.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Ts.Equal(ts) {
		t.Errorf("Ts = %v, want an instant Equal to %v", got.Ts, ts)
	}
}

// Card A3 (with the slice-2 bound fix): UnixNano wraps silently outside
// time.Time's representable range, so Put refuses a Ts there — with the true
// limits [1677-09-21T00:12:43.145224192Z, 2262-04-11T23:47:16.854775807Z]
// named — instead of storing a wrong instant; the bounds themselves still
// round-trip.
func TestPutRefusesOutOfRangeTs(t *testing.T) {
	s := openTemp(t)
	_, err := s.Put(&memstore.Memory{Kind: "note", Text: "year 2300", Ts: time.Date(2300, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err == nil {
		t.Error("Put with Ts year 2300: got nil error, want refusal")
	} else if got := err.Error(); !strings.Contains(got, "1677") || !strings.Contains(got, "2262") {
		t.Errorf("refusal %q does not name the timestamp limits", got)
	}
	// A nanosecond below the true lower bound (or above the upper one) must be
	// refused, never wrapped.
	for _, ts := range []time.Time{
		time.Unix(0, math.MinInt64).Add(-time.Nanosecond),
		time.Unix(0, math.MaxInt64).Add(time.Nanosecond),
	} {
		if _, err := s.Put(&memstore.Memory{Kind: "note", Text: "past the nanosecond edge", Ts: ts}); err == nil {
			t.Errorf("Put just outside the range (%v): got nil error, want refusal", ts)
		} else if got := err.Error(); !strings.Contains(got, "1677") || !strings.Contains(got, "2262") {
			t.Errorf("refusal %q does not name the timestamp limits", got)
		}
	}
	for _, ts := range []time.Time{
		time.Date(1677, 9, 21, 0, 12, 43, 145224192, time.UTC),
		time.Date(2262, 4, 11, 23, 47, 16, 854775807, time.UTC),
	} {
		id, err := s.Put(&memstore.Memory{Kind: "note", Text: "at the nanosecond boundary", Ts: ts})
		if err != nil {
			t.Fatalf("Put at boundary %v: %v", ts, err)
		}
		got, err := s.Get(id)
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if !got.Ts.Equal(ts) {
			t.Errorf("Ts = %v, want Equal to boundary %v", got.Ts, ts)
		}
	}
}

// Card F2 / design Security: while the store is open, the file and its WAL
// sidecars are 0600, and the parent directory that Open creates is 0700 — even
// under a permissive umask. It does not claim a caller's pre-existing
// directory is touched; Open leaves that as it is.
// Card 2 / spec AC2: Close retires a record — status closed, text intact, no
// deleted_at. With a replacement the result carries it: on the supersedes
// column and, because Close records the superseded-by edge, on the search
// walk's Related list too.
func TestCloseSetsStatusAndReplaces(t *testing.T) {
	s := openTemp(t)
	d1, err := s.Put(&memstore.Memory{Kind: "decision", Text: "closeable-quokka-plan"})
	if err != nil {
		t.Fatalf("Put D1: %v", err)
	}
	d2, err := s.Put(&memstore.Memory{Kind: "decision", Text: "the successor plan, sünnset"})
	if err != nil {
		t.Fatalf("Put D2: %v", err)
	}

	if err := s.CloseMemory(d1, &d2); err != nil {
		t.Fatalf("CloseMemory: %v", err)
	}
	got, err := s.Get(d1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "closed" {
		t.Errorf("Status = %q, want closed", got.Status)
	}
	if got.Text != "closeable-quokka-plan" {
		t.Errorf("Text changed by Close: %q", got.Text)
	}
	if got.Supersedes == nil || *got.Supersedes != d2 {
		t.Errorf("Supersedes = %v, want %d", got.Supersedes, d2)
	}
	if got.DeletedAt != nil {
		t.Error("Close touched deleted_at; only Forget may")
	}

	hits, err := s.Search("closeable-quokka", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("Search found %d hits, want the closed row %d", len(hits), d1)
	}
	// Spec AC2: the hit carries the successor's id.
	if hits[0].Supersedes == nil || *hits[0].Supersedes != d2 {
		t.Errorf("search hit Supersedes = %v, want %d", hits[0].Supersedes, d2)
	}
	if !hasRel(hits[0].Related, "superseded-by", d2) {
		t.Errorf("search hit Related = %v, want the superseded-by edge to %d", hits[0].Related, d2)
	}
}

// Card 2: unknown id and a forgotten row are errors; a second Close is a
// no-op — the first close owns the state.
func TestCloseErrorsAndSecondNoOp(t *testing.T) {
	s := openTemp(t)
	if err := s.CloseMemory(424242, nil); err == nil {
		t.Error("CloseMemory(unknown id): got nil error, want error")
	}
	d, err := s.Put(&memstore.Memory{Kind: "note", Text: "double-closeable"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.CloseMemory(d, nil); err != nil {
		t.Fatalf("CloseMemory: %v", err)
	}
	if err := s.CloseMemory(d, nil); err != nil {
		t.Errorf("second CloseMemory: %v, want no-op (nil)", err)
	}
	got, err := s.Get(d)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "closed" {
		t.Errorf("Status = %q after no-op second close, want closed", got.Status)
	}

	// A replacement that does not name a live memory must refuse and change
	// nothing — fail closed, never half-written.
	healthy, _ := s.Put(&memstore.Memory{Kind: "note", Text: "must-not-be-replaced-by-void"})
	missing := int64(99991)
	if err := s.CloseMemory(healthy, &missing); err == nil {
		t.Error("CloseMemory with unknown replacement: got nil error, want refusal")
	}
	got, err = s.Get(healthy)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "open" || got.Supersedes != nil {
		t.Errorf("Close with bad replacement half-wrote: status=%q supersedes=%v", got.Status, got.Supersedes)
	}

	// A forgotten row is hidden from the verbs, as if missing.
	f, _ := s.Put(&memstore.Memory{Kind: "note", Text: "forgotten-then-closed"})
	if err := s.Forget(f); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if err := s.CloseMemory(f, nil); err == nil {
		t.Error("CloseMemory of a forgotten row: got nil error, want refusal")
	}
}

// Card 3 / spec AC4: Forget hides the row from Get, Search and Recent, keeps
// its bytes, and leaves the db file's size unchanged.
func TestForgetHidesAndKeepsBytes(t *testing.T) {
	s := openTemp(t)
	id, err := s.Put(&memstore.Memory{Kind: "note", Text: "forgettable-payload-line1\nline2"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	keep, err := s.Put(&memstore.Memory{Kind: "note", Text: "still-live-sibling"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}

	before, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("Stat db before forget: %v", err)
	}
	if err := s.Forget(id); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	if _, err := s.Get(id); err == nil {
		t.Error("Get of a forgotten row: got nil error, want hidden")
	}
	hits, err := s.Search("forgettable-payload", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("Search found %d hits for a forgotten row, want 0", len(hits))
	}
	rec, err := s.Recent("", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	for _, r := range rec {
		if r.Id == id {
			t.Errorf("Recent returned the forgotten row %d", id)
		}
	}
	if len(rec) != 1 || rec[0].Id != keep {
		t.Errorf("Recent = %v, want only the live row %d", rec, keep)
	}
	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Total != 2 || st.Open != 1 || st.Forgotten != 1 {
		t.Errorf("Stats = total %d open %d forgotten %d, want 2/1/1", st.Total, st.Open, st.Forgotten)
	}

	after, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("Stat db after forget: %v", err)
	}
	if before.Size() != after.Size() {
		t.Errorf("db file size changed by forget: %d -> %d; bytes must remain", before.Size(), after.Size())
	}
	// The bytes remain on the row itself — read past the verb surface, which
	// hides it by design.
	db, err := sql.Open("sqlite", s.Path())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	var text string
	if err := db.QueryRow(`SELECT text FROM memory WHERE id = ?`, id).Scan(&text); err != nil {
		t.Fatalf("row gone after forget: %v", err)
	}
	if text != "forgettable-payload-line1\nline2" {
		t.Errorf("bytes not intact after forget: %q", text)
	}
}

// Card 3: forget of an unknown or already-forgotten id is an error.
func TestForgetUnknownAndTwice(t *testing.T) {
	s := openTemp(t)
	if err := s.Forget(424242); err == nil {
		t.Error("Forget(unknown id): got nil error, want error")
	}
	id, err := s.Put(&memstore.Memory{Kind: "note", Text: "forget-me-twice"})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Forget(id); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if err := s.Forget(id); err == nil {
		t.Error("second Forget: got nil error, want error")
	}
}

// Card 4: Recent is newest-ts-first, scope-AND-ed, hides forgotten rows, and
// k <= 0 is empty. It orders by time, not by insertion.
func TestRecentNewestFirstAndScope(t *testing.T) {
	s := openTemp(t)
	ts := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
	}
	old, err := s.Put(&memstore.Memory{Kind: "note", Scope: "memstore", Text: "recent-old", Ts: ts(2026, 10, 1)})
	if err != nil {
		t.Fatalf("Put old: %v", err)
	}
	mid, err := s.Put(&memstore.Memory{Kind: "note", Scope: "other", Text: "recent-mid", Ts: ts(2026, 10, 2)})
	if err != nil {
		t.Fatalf("Put mid: %v", err)
	}
	nw, err := s.Put(&memstore.Memory{Kind: "note", Scope: "memstore", Text: "recent-new", Ts: ts(2026, 10, 3)})
	if err != nil {
		t.Fatalf("Put new: %v", err)
	}

	rec, err := s.Recent("", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(rec) != 3 || rec[0].Id != nw || rec[1].Id != mid || rec[2].Id != old {
		t.Errorf("Recent = %v, want newest-first [%d %d %d]", ids(rec), nw, mid, old)
	}

	only, err := s.Recent("memstore", 10)
	if err != nil {
		t.Fatalf("Recent(memstore): %v", err)
	}
	if len(only) != 2 || only[0].Id != nw || only[1].Id != old {
		t.Errorf("Recent(scoped) = %v, want [%d %d]", ids(only), nw, old)
	}

	if err := s.Forget(mid); err != nil {
		t.Fatalf("Forget mid: %v", err)
	}
	rec, err = s.Recent("", 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(rec) != 2 || rec[0].Id != nw || rec[1].Id != old {
		t.Errorf("Recent after forget = %v, want [%d %d]", ids(rec), nw, old)
	}

	for _, k := range []int{0, -1} {
		rec, err := s.Recent("", k)
		if err != nil {
			t.Fatalf("Recent(k=%d): %v", k, err)
		}
		if len(rec) != 0 {
			t.Errorf("Recent(k=%d) = %v, want empty", k, rec)
		}
	}
}

// Card 5 / spec AC2: the relation walk — a Search hit surfaces the ids its
// outgoing superseded-by and found-by edges name.
func TestSearchWalkSurfacesEdges(t *testing.T) {
	s := openTemp(t)
	a, err := s.Put(&memstore.Memory{Kind: "note", Text: "walkable-original-mombai"})
	if err != nil {
		t.Fatalf("Put A: %v", err)
	}
	b, err := s.Put(&memstore.Memory{Kind: "note", Text: "the successor to the original"})
	if err != nil {
		t.Fatalf("Put B: %v", err)
	}
	src, err := s.Put(&memstore.Memory{Kind: "note", Text: "a journal where the original was found"})
	if err != nil {
		t.Fatalf("Put src: %v", err)
	}
	if err := s.Link(a, "superseded-by", b); err != nil {
		t.Fatalf("Link superseded-by: %v", err)
	}
	if err := s.Link(a, "found-by", src); err != nil {
		t.Fatalf("Link found-by: %v", err)
	}

	hits, err := s.Search("walkable-original", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("Search found %d hits, want 1", len(hits))
	}
	if hits[0].Id != a {
		t.Fatalf("hit id = %d, want %d", hits[0].Id, a)
	}
	if got := len(hits[0].Related); got != 2 {
		t.Errorf("walk surfaced %d relations %v, want the 2 edges", got, hits[0].Related)
	}
	if !hasRel(hits[0].Related, "superseded-by", b) || !hasRel(hits[0].Related, "found-by", src) {
		t.Errorf("walk surfaced %v, want (superseded-by,%d) and (found-by,%d)", hits[0].Related, b, src)
	}
}

// Card 5: Link refuses unknown or forgotten endpoints; a repeated identical
// Link is a no-op — one edge, not two.
func TestLinkValidationAndRepeatNoOp(t *testing.T) {
	s := openTemp(t)
	a, err := s.Put(&memstore.Memory{Kind: "note", Text: "linkable-a"})
	if err != nil {
		t.Fatalf("Put A: %v", err)
	}
	b, err := s.Put(&memstore.Memory{Kind: "note", Text: "linkable-b"})
	if err != nil {
		t.Fatalf("Put B: %v", err)
	}
	if err := s.Link(424242, "superseded-by", b); err == nil {
		t.Error("Link with unknown from: got nil error, want error")
	}
	if err := s.Link(a, "superseded-by", 424242); err == nil {
		t.Error("Link with unknown to: got nil error, want error")
	}
	f, _ := s.Put(&memstore.Memory{Kind: "note", Text: "linkable-forgotten"})
	if err := s.Forget(f); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if err := s.Link(a, "found-by", f); err == nil {
		t.Error("Link to a forgotten endpoint: got nil error, want error")
	}

	if err := s.Link(a, "superseded-by", b); err != nil {
		t.Fatalf("first Link: %v", err)
	}
	if err := s.Link(a, "superseded-by", b); err != nil {
		t.Fatalf("repeated Link: %v", err)
	}
	db, err := sql.Open("sqlite", s.Path())
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM edge WHERE from_id = ? AND rel = 'superseded-by' AND to_id = ?`, a, b).Scan(&n); err != nil {
		t.Fatalf("count edges: %v", err)
	}
	if n != 1 {
		t.Errorf("edge count = %d, want 1 (repeated Link must be a no-op)", n)
	}
}

// Card 6: Stats reports the store's own shape — counts over all rows, the db
// file's byte size, and the newest and oldest ts present (including on rows
// the verbs hide, because their bytes are still in the file).
func TestStatsCountsBytesAndTsBounds(t *testing.T) {
	s := openTemp(t)
	ts := func(y int, m time.Month, d int) time.Time {
		return time.Date(y, m, d, 12, 0, 0, 0, time.UTC)
	}
	oldestTs, newestTs, forgottenTs := ts(2025, 1, 1), ts(2026, 10, 1), ts(2024, 6, 1)
	if _, err := s.Put(&memstore.Memory{Kind: "note", Text: "stats-oldest", Ts: oldestTs}); err != nil {
		t.Fatalf("Put oldest: %v", err)
	}
	if _, err := s.Put(&memstore.Memory{Kind: "note", Text: "stats-newest", Ts: newestTs}); err != nil {
		t.Fatalf("Put newest: %v", err)
	}
	forg, err := s.Put(&memstore.Memory{Kind: "note", Text: "stats-forgotten", Ts: forgottenTs})
	if err != nil {
		t.Fatalf("Put forgotten: %v", err)
	}
	if err := s.Forget(forg); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Total != 3 || st.Open != 2 || st.Forgotten != 1 {
		t.Errorf("Stats = total %d open %d forgotten %d, want 3/2/1", st.Total, st.Open, st.Forgotten)
	}
	fi, err := os.Stat(s.Path())
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if st.Bytes != fi.Size() {
		t.Errorf("Stats bytes = %d, want the actual file size %d", st.Bytes, fi.Size())
	}
	// The ts bounds span every row's bytes, hidden or not.
	if st.Newest == nil || !st.Newest.Equal(newestTs) {
		t.Errorf("Newest = %v, want %v", st.Newest, newestTs)
	}
	if st.Oldest == nil || !st.Oldest.Equal(forgottenTs) {
		t.Errorf("Oldest = %v, want the forgotten row's %v", st.Oldest, forgottenTs)
	}

	// An empty store has counts of zero, no ts bounds, and a real file size.
	empty := openTemp(t)
	e, err := empty.Stats()
	if err != nil {
		t.Fatalf("Stats(empty): %v", err)
	}
	if e.Total != 0 || e.Open != 0 || e.Forgotten != 0 {
		t.Errorf("Stats(empty) = total %d open %d forgotten %d, want 0/0/0", e.Total, e.Open, e.Forgotten)
	}
	if e.Newest != nil || e.Oldest != nil {
		t.Errorf("Stats(empty) ts bounds = %v/%v, want nil", e.Newest, e.Oldest)
	}
	if e.Bytes == 0 {
		// the file exists and carries schema pages, so the size is nonzero
		t.Errorf("Stats(empty) bytes = 0, want the real file size")
	}
}

func ids(ms []memstore.Memory) []int64 {
	out := make([]int64, len(ms))
	for i, m := range ms {
		out[i] = m.Id
	}
	return out
}

func hasRel(rs []memstore.Related, rel string, to int64) bool {
	for _, r := range rs {
		if r.Rel == rel && r.To == to {
			return true
		}
	}
	return false
}

func TestStorePermissionsUserOnly(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)

	path := filepath.Join(t.TempDir(), "sub", "memory.db")
	s, err := memstore.Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()
	if _, err := s.Put(&memstore.Memory{Kind: "note", Text: "a write, so the WAL sidecars exist"}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Errorf("Stat %s: %v", filepath.Base(p), err)
			continue
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode = %o, want 600", filepath.Base(p), got)
		}
	}
	di, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if got := di.Mode().Perm(); got != 0o700 {
		t.Errorf("db dir mode = %o, want 700", got)
	}
}
