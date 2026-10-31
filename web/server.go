// Package web serves a read-only view of one memstore file on the loopback
// interface. It owns routing, request validation, response bounds, security
// headers and the embedded page; every value it returns comes from an exported
// memstore method, and this package opens no database of its own.
package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"

	"github.com/IanLuo/memstore"
)

//go:embed index.html
var page embed.FS

// DefaultLimit is the page size a request without a limit gets; MaxQueryLen
// bounds the text query so one request cannot ask the index for a novel.
const (
	DefaultLimit = 20
	MaxQueryLen  = 512
)

// Handler serves the UI and its read-only API over store. It answers GET and
// HEAD only; any other method gets 405 from the route patterns below.
func Handler(store *memstore.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", servePage)
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		serveStats(w, store)
	})
	mux.HandleFunc("GET /api/memories", func(w http.ResponseWriter, r *http.Request) {
		serveMemories(w, r, store)
	})
	mux.HandleFunc("GET /api/memories/{id}", func(w http.ResponseWriter, r *http.Request) {
		serveMemory(w, r, store)
	})
	return guard(mux)
}

// guard refuses a Host that is not loopback, then adds the response headers
// every answer carries. The listener is already bound to 127.0.0.1; the Host
// check is what keeps a DNS-rebinding page from reaching it under its own name.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !loopbackHost(r.Host) {
			http.Error(w, "memstore: host not allowed", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'none'; style-src 'unsafe-inline'; script-src 'unsafe-inline'; "+
				"img-src data:; connect-src 'self'; base-uri 'none'; form-action 'none'")
		h.Set("Cache-Control", "no-store")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// loopbackHost reports whether a request's Host names this machine's loopback
// interface, with or without a port.
func loopbackHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	switch host {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func servePage(w http.ResponseWriter, r *http.Request) {
	body, err := page.ReadFile("index.html")
	if err != nil {
		http.Error(w, "memstore: read page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(body)
}

// statsResponse separates what the store measured from what this process is
// configured with. Embedder is always false: serve never registers one.
type statsResponse struct {
	Store    memstore.Stats `json:"store"`
	Embedder bool           `json:"embedder"`
}

func serveStats(w http.ResponseWriter, store *memstore.Store) {
	st, err := store.Stats()
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, statsResponse{Store: st, Embedder: false})
}

// memoriesResponse is one page of results. HasMore is answered by the store
// fetching one row beyond the page, not by a separate count.
type memoriesResponse struct {
	Memories []memstore.Memory `json:"memories"`
	HasMore  bool              `json:"hasMore"`
}

func serveMemories(w http.ResponseWriter, r *http.Request, store *memstore.Store) {
	q := r.URL.Query()
	text := q.Get("q")
	if len(text) > MaxQueryLen {
		http.Error(w, fmt.Sprintf("memstore: query longer than %d bytes", MaxQueryLen), http.StatusBadRequest)
		return
	}
	limit, err := intParam(q.Get("limit"), DefaultLimit)
	if err != nil {
		http.Error(w, "memstore: "+err.Error(), http.StatusBadRequest)
		return
	}
	if limit < 1 || limit > memstore.MaxPageSize {
		http.Error(w, fmt.Sprintf("memstore: limit must be 1..%d", memstore.MaxPageSize), http.StatusBadRequest)
		return
	}
	offset, err := intParam(q.Get("offset"), 0)
	if err != nil {
		http.Error(w, "memstore: "+err.Error(), http.StatusBadRequest)
		return
	}
	filter := memstore.Filter{
		Kind:   q.Get("kind"),
		Scope:  q.Get("scope"),
		Status: q.Get("status"),
		Origin: q.Get("origin"),
	}
	hits, hasMore, err := store.Query(text, filter, limit, offset)
	if err != nil {
		fail(w, err)
		return
	}
	if hits == nil {
		hits = []memstore.Memory{}
	}
	writeJSON(w, memoriesResponse{Memories: hits, HasMore: hasMore})
}

// relationView is one stored edge with its target's label, resolved so a page
// needs one request. A target the store will not show is unavailable, and
// carries no title: the edge is reported, its content is not invented.
type relationView struct {
	Rel       string `json:"rel"`
	To        int64  `json:"to"`
	Title     string `json:"title,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Available bool   `json:"available"`
}

type memoryResponse struct {
	Memory    memstore.Memory `json:"memory"`
	Relations []relationView  `json:"relations"`
}

func serveMemory(w http.ResponseWriter, r *http.Request, store *memstore.Store) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.Error(w, "memstore: bad memory id", http.StatusBadRequest)
		return
	}
	m, err := store.Get(id)
	if err != nil {
		fail(w, err)
		return
	}
	edges, err := store.Relations(id)
	if err != nil {
		fail(w, err)
		return
	}
	out := memoryResponse{Memory: m, Relations: make([]relationView, 0, len(edges))}
	for _, edge := range edges {
		view := relationView{Rel: edge.Rel, To: edge.To}
		if target, err := store.Get(edge.To); err == nil {
			view.Title, view.Kind, view.Available = target.Title, target.Kind, true
		}
		out.Relations = append(out.Relations, view)
	}
	writeJSON(w, out)
}

// intParam reads a non-negative integer parameter, defaulting when it is absent
// and refusing anything else: a caller that sends a bad page has a bug.
func intParam(raw string, def int) (int, error) {
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number", raw)
	}
	if n < 0 {
		return 0, fmt.Errorf("%q is negative", raw)
	}
	return n, nil
}

// isInputError is gone: bounds are checked here, so a Store.Query error is the
// store's own failure and answers 500.

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

// fail answers a store failure. The detail goes to the local stderr, which the
// operator runs; the client gets a message with no memory text and no query.
func fail(w http.ResponseWriter, err error) {
	log.Printf("web: %v", err)
	http.Error(w, "memstore: read failed", http.StatusInternalServerError)
}
