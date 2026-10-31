package web_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/IanLuo/memstore"
	"github.com/IanLuo/memstore/web"
)

func fixture(t *testing.T) *memstore.Store {
	t.Helper()
	s, err := memstore.Open(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	for _, m := range []memstore.Memory{
		{Kind: "decision", Scope: "alpha", Title: "Queue drains on restart", Text: "the queue drains when the worker restarts", Origin: "native"},
		{Kind: "knowledge", Scope: "alpha", Title: "Worker start", Text: "worker start clears the queue", Origin: "native"},
		{Kind: "note", Scope: "beta", Title: "<script>alert(1)</script>", Text: "markup <b>bold</b> & queue", Origin: "native"},
	} {
		if _, err := s.Put(&m); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}
	return s
}

// get issues a request the way the browser does: loopback Host, GET.
func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = "127.0.0.1:8765"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Spec: the page is served from the binary with no external asset, and every
// answer carries the security and cache headers.
func TestPageServedWithHeaders(t *testing.T) {
	h := web.Handler(fixture(t))
	rec := get(t, h, "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, "<title>memstore</title>") {
		t.Error("page body does not contain the UI")
	}
	if body := rec.Body.String(); strings.Contains(body, "http://") || strings.Contains(body, "https://") {
		t.Error("page references an external URL")
	}
	for _, hdr := range []string{"Content-Security-Policy", "Cache-Control", "X-Content-Type-Options", "Referrer-Policy"} {
		if rec.Header().Get(hdr) == "" {
			t.Errorf("%s is missing", hdr)
		}
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Content-Security-Policy"); strings.Contains(got, "unsafe-eval") || strings.Contains(got, "http") {
		t.Errorf("CSP allows something it should not: %q", got)
	}
}

// Spec: a Host that is not loopback is refused before routing, so a
// rebinding page cannot reach the store under its own name.
func TestForeignHostRefused(t *testing.T) {
	h := web.Handler(fixture(t))
	for _, host := range []string{"evil.example", "10.0.0.5:8765", "0.0.0.0:8765"} {
		req := httptest.NewRequest(http.MethodGet, "/api/memories", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", host, rec.Code)
		}
	}
	for _, host := range []string{"localhost:8765", "127.0.0.1:8765", "[::1]:8765"} {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("Host %q = %d, want 200", host, rec.Code)
		}
	}
}

// Spec: the surface is read-only. Every write verb is refused, and no route
// accepts one.
func TestWritesRefused(t *testing.T) {
	h := web.Handler(fixture(t))
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, target := range []string{"/", "/api/memories", "/api/memories/1", "/api/stats"} {
			req := httptest.NewRequest(method, target, strings.NewReader(`{"text":"x"}`))
			req.Host = "127.0.0.1:8765"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", method, target, rec.Code)
			}
		}
	}
}

// Spec: the overview reports what Store.Stats measured, and the server states
// that it configures no embedder.
func TestStatsMatchStore(t *testing.T) {
	s := fixture(t)
	want, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	rec := get(t, web.Handler(s), "/api/stats")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/stats = %d, want 200", rec.Code)
	}
	var got struct {
		Store    memstore.Stats `json:"store"`
		Embedder bool           `json:"embedder"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Store.Total != want.Total || got.Store.Open != want.Open || got.Store.Bytes != want.Bytes {
		t.Errorf("stats = %+v, want %+v", got.Store, want)
	}
	if got.Embedder {
		t.Error("Embedder = true; serve never configures one")
	}
}

// Spec: filters reach the store, so the API returns only matching rows.
func TestMemoriesFiltered(t *testing.T) {
	h := web.Handler(fixture(t))
	rec := get(t, h, "/api/memories?scope=beta")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var got struct {
		Memories []memstore.Memory `json:"memories"`
		HasMore  bool              `json:"hasMore"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Memories) != 1 || got.Memories[0].Scope != "beta" {
		t.Fatalf("scope=beta = %+v, want the single beta row", got.Memories)
	}
	if got.HasMore {
		t.Error("hasMore = true with one row")
	}
}

// Spec: bad pages and over-long queries are refused with 400, not clamped.
func TestBadRequestsRefused(t *testing.T) {
	h := web.Handler(fixture(t))
	for _, target := range []string{
		"/api/memories?limit=0",
		"/api/memories?limit=101",
		"/api/memories?limit=abc",
		"/api/memories?offset=-1",
		"/api/memories?q=" + strings.Repeat("x", web.MaxQueryLen+1),
		"/api/memories/not-a-number",
	} {
		if rec := get(t, h, target); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", target, rec.Code)
		}
	}
}

// Spec: memory text is data, not markup. The JSON encoder escapes it, and the
// UI renders it as text, so a stored script tag cannot execute.
func TestMemoryTextIsNotMarkup(t *testing.T) {
	h := web.Handler(fixture(t))
	rec := get(t, h, "/api/memories?kind=note")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "<script>") {
		t.Errorf("raw script tag in response: %s", body)
	}
	if !strings.Contains(body, `\u003cscript\u003e`) {
		t.Errorf("script tag was neither escaped nor omitted: %s", body)
	}
}

// Spec: one request returns the detail with its relations resolved, and an
// edge to a hidden memory is reported as unavailable.
func TestDetailResolvesRelations(t *testing.T) {
	s := openPair(t)
	oldID, newID := pairIDs(t, s)
	if err := s.CloseMemory(oldID, &newID); err != nil {
		t.Fatalf("CloseMemory: %v", err)
	}
	h := web.Handler(s)

	rec := get(t, h, "/api/memories/"+itoa(oldID))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	var got detail
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Memory.Id != oldID {
		t.Errorf("memory id = %d, want %d", got.Memory.Id, oldID)
	}
	if len(got.Relations) != 1 || got.Relations[0].To != newID || !got.Relations[0].Available || got.Relations[0].Title == "" {
		t.Fatalf("relations = %+v, want one available edge to %d with a title", got.Relations, newID)
	}

	if err := s.Forget(newID); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	rec = get(t, h, "/api/memories/"+itoa(oldID))
	got = detail{}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Relations) != 1 || got.Relations[0].Available || got.Relations[0].Title != "" {
		t.Fatalf("relations after forget = %+v, want the edge reported unavailable with no title", got.Relations)
	}
}

// detail mirrors the JSON the UI receives for one memory.
type detail struct {
	Memory    memstore.Memory `json:"memory"`
	Relations []struct {
		Rel       string `json:"rel"`
		To        int64  `json:"to"`
		Title     string `json:"title"`
		Available bool   `json:"available"`
	} `json:"relations"`
}

// openPair and its helpers keep TestDetailResolvesRelations readable without
// reaching into the store's internals.
func openPair(t *testing.T) *memstore.Store {
	t.Helper()
	s, err := memstore.Open(t.TempDir() + "/memory.db")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func pairIDs(t *testing.T, s *memstore.Store) (int64, int64) {
	t.Helper()
	oldID, err := s.Put(&memstore.Memory{Kind: "decision", Title: "Old", Text: "old decision"})
	if err != nil {
		t.Fatalf("Put old: %v", err)
	}
	newID, err := s.Put(&memstore.Memory{Kind: "decision", Title: "New", Text: "new decision"})
	if err != nil {
		t.Fatalf("Put new: %v", err)
	}
	return oldID, newID
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
