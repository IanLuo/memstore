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
	"strings"

	"github.com/IanLuo/memstore"
)

//go:embed index.html
var page embed.FS

const maxQueryLen = 512

// Handler serves the UI and the same read operations exposed by the CLI:
// Search, Recent, Get and Stats. It does not create a second data interface.
func Handler(store *memstore.Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", servePage)
	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		st, err := store.Stats()
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, st)
	})
	mux.HandleFunc("GET /api/search", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		if q == "" {
			http.Error(w, "memstore: search: empty query", http.StatusBadRequest)
			return
		}
		if len(q) > maxQueryLen {
			http.Error(w, fmt.Sprintf("memstore: query longer than %d bytes", maxQueryLen), http.StatusBadRequest)
			return
		}
		k, err := intParam(r.URL.Query().Get("k"), 10)
		if err != nil {
			http.Error(w, "memstore: search: "+err.Error(), http.StatusBadRequest)
			return
		}
		hits, err := store.Search(q, k)
		if err != nil {
			fail(w, err)
			return
		}
		if hits == nil {
			hits = []memstore.Memory{}
		}
		writeJSON(w, hits)
	})
	mux.HandleFunc("GET /api/recent", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		k, err := intParam(q.Get("k"), 20)
		if err != nil {
			http.Error(w, "memstore: recent: "+err.Error(), http.StatusBadRequest)
			return
		}
		scope := q.Get("scope")
		if len(scope) > maxQueryLen {
			http.Error(w, "memstore: recent: scope is too long", http.StatusBadRequest)
			return
		}
		rows, err := store.Recent(scope, k)
		if err != nil {
			fail(w, err)
			return
		}
		if rows == nil {
			rows = []memstore.Memory{}
		}
		writeJSON(w, rows)
	})
	mux.HandleFunc("GET /api/get", func(w http.ResponseWriter, r *http.Request) {
		idValue, ok := r.URL.Query()["id"]
		if !ok || len(idValue) == 0 {
			http.Error(w, "memstore: get: missing id", http.StatusBadRequest)
			return
		}
		id, err := strconv.ParseInt(idValue[0], 10, 64)
		if err != nil || id <= 0 {
			http.Error(w, "memstore: get: bad id", http.StatusBadRequest)
			return
		}
		m, err := store.Get(id)
		if err != nil {
			fail(w, err)
			return
		}
		writeJSON(w, m)
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
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return strings.EqualFold(host, "localhost")
}

func servePage(w http.ResponseWriter, r *http.Request) {
	body, err := page.ReadFile("index.html")
	if err != nil {
		http.Error(w, "memstore: read page", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body)
}

// intParam reads a non-negative integer parameter, defaulting when it is absent.
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

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(true)
	_ = enc.Encode(v)
}

// fail answers a Store failure without echoing memory contents to the browser.
func fail(w http.ResponseWriter, err error) {
	log.Printf("web: %v", err)
	http.Error(w, "memstore: read failed", http.StatusInternalServerError)
}
