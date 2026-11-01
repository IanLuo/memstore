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

func get(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Host = "127.0.0.1:8765"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPageServedWithHeaders(t *testing.T) {
	rec := get(t, web.Handler(fixture(t)), "/")
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("GET / = %d, content-type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "Projects &amp; tasks") || !strings.Contains(rec.Body.String(), "Agent retrieval") {
		t.Error("page does not contain the store and agent views")
	}
	if strings.Contains(rec.Body.String(), "http://") || strings.Contains(rec.Body.String(), "https://") {
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
}

func TestForeignHostRefused(t *testing.T) {
	h := web.Handler(fixture(t))
	for _, host := range []string{"evil.example", "10.0.0.5:8765", "0.0.0.0:8765"} {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("Host %q = %d, want 403", host, rec.Code)
		}
	}
	for _, host := range []string{"localhost:8765", "127.0.0.1:8765", "[::1]:8765", "127.0.0.2:8765"} {
		req := httptest.NewRequest(http.MethodGet, "/api/stats", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("Host %q = %d, want 200", host, rec.Code)
		}
	}
}

func TestWritesRefused(t *testing.T) {
	h := web.Handler(fixture(t))
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		for _, target := range []string{"/", "/api/search?q=queue&k=10", "/api/recent?k=10", "/api/get?id=1", "/api/stats"} {
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

func TestStatsMatchStore(t *testing.T) {
	s := fixture(t)
	want, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	rec := get(t, web.Handler(s), "/api/stats")
	var got memstore.Stats
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/stats = %d", rec.Code)
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Total != want.Total || got.Open != want.Open || got.Bytes != want.Bytes || got.WalBytes != want.WalBytes {
		t.Errorf("stats = %+v, want %+v", got, want)
	}
	if len(got.Scopes) != len(want.Scopes) {
		t.Fatalf("Scopes = %+v, want %+v", got.Scopes, want.Scopes)
	}
	for i := range want.Scopes {
		if got.Scopes[i] != want.Scopes[i] {
			t.Errorf("Scopes[%d] = %+v, want %+v", i, got.Scopes[i], want.Scopes[i])
		}
	}
}

func TestSearchMatchesPublicStoreSearch(t *testing.T) {
	s := fixture(t)
	expected, err := s.Search("queue", 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	rec := get(t, web.Handler(s), "/api/search?q=queue&k=2")
	var got []memstore.Memory
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(expected) {
		t.Fatalf("HTTP returned %d rows, Store.Search returned %d", len(got), len(expected))
	}
	for i := range expected {
		if got[i].Id != expected[i].Id {
			t.Errorf("result %d id %d, Store.Search id %d", i, got[i].Id, expected[i].Id)
		}
	}
}

func TestRecentMatchesPublicStoreRecent(t *testing.T) {
	s := fixture(t)
	expected, err := s.Recent("alpha", 2)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	rec := get(t, web.Handler(s), "/api/recent?scope=alpha&k=2")
	var got []memstore.Memory
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != len(expected) {
		t.Fatalf("HTTP returned %d rows, Store.Recent returned %d", len(got), len(expected))
	}
	for i := range expected {
		if got[i].Id != expected[i].Id {
			t.Errorf("recent %d id %d, Store.Recent id %d", i, got[i].Id, expected[i].Id)
		}
	}
}

func TestGetMatchesPublicStoreGet(t *testing.T) {
	s := fixture(t)
	recent, err := s.Recent("beta", 1)
	if err != nil || len(recent) != 1 {
		t.Fatalf("Recent beta: rows=%d err=%v", len(recent), err)
	}
	expected, err := s.Get(recent[0].Id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	rec := get(t, web.Handler(s), "/api/get?id="+itoa(expected.Id))
	var got memstore.Memory
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Id != expected.Id || got.Text != expected.Text {
		t.Fatalf("HTTP Get = %+v, want %+v", got, expected)
	}
}

func TestBadReadRequestsRefused(t *testing.T) {
	h := web.Handler(fixture(t))
	for _, target := range []string{
		"/api/search",
		"/api/recent?k=abc",
		"/api/get",
		"/api/search?q=" + strings.Repeat("x", 513),
		"/api/search?q=queue&k=abc",
		"/api/recent?k=abc",
		"/api/recent?scope=" + strings.Repeat("x", 513),
		"/api/get",
		"/api/get?id=-1",
		"/api/get?id=abc",
	} {
		if rec := get(t, h, target); rec.Code != http.StatusBadRequest {
			t.Errorf("GET %s = %d, want 400", target, rec.Code)
		}
	}
}

func TestMemoryTextIsEscapedInJSON(t *testing.T) {
	s := fixture(t)
	hit, err := s.Search("markup", 5)
	if err != nil || len(hit) != 1 {
		t.Fatalf("Search markup: rows=%d err=%v", len(hit), err)
	}
	rec := get(t, web.Handler(s), "/api/get?id="+itoa(hit[0].Id))
	if strings.Contains(rec.Body.String(), "<script>") || !strings.Contains(rec.Body.String(), `\u003cscript\u003e`) {
		t.Errorf("memory markup not JSON-escaped: %s", rec.Body.String())
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
