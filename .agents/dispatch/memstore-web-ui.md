# Dispatch — local read-only web UI and fresh-agent handoff

## Contract
- Upstream: `.agents/specs/memory-store.md`, `.agents/specs/memory-store-design.md`.
- Implement only after both contracts are relocked for this slice.
- Do not touch the other untracked dispatch cards or `.playwright-mcp/` artifacts.

## Outcome
- `memstore serve` serves a read-only UI on `127.0.0.1:8765` by default; optional port override, no bind-address override.
- UI reads one store through public `memstore` package methods only; HTTP handlers contain no SQL/SQLite access.
- UI provides live store stats, real combined search and filter-only listing, memory detail/relations, agent candidate view, and client-side handoff packet builder with copy/download and manual fresh-agent dry-run fields.
- No UI/server memory mutations, persistence of handoff drafts, agent telemetry, external requests, or external assets.

## Boundaries
- Add Store-level structured query/list APIs; apply filters before limiting; preserve FTS BM25 ordering; filter-only order is `(ts DESC, id DESC)`; hidden rows excluded; bounded deterministic pagination.
- Extend Matchers only as needed to keep ranking semantics under filters. Do not post-filter a top-k result in HTTP.
- Reuse `Store.Stats`, `Search`, `Recent`, `Get`, and returned `Memory`/`Related` data where sufficient.
- Browser input cannot select a database path. Use `MEMSTORE_DB` or the existing home default.
- Host allowlist accepts loopback URL hosts only; GET/HEAD only; same origin; restrictive CSP; no-store memory responses; bounded inputs; no content/query logging.
- Handoff checkout/test fields are operator-entered and explicitly unverified until checked. Selected memory source text is exact and identified. Fresh-agent response/review fields stay client-side and are excluded from handoff export.

## Verification
- Package tests: combined exact filters, text rank preservation, filter-only ordering, hidden rows, stable bounded pagination, invalid inputs, relations/stats.
- HTTP tests: loopback serving, host/method restrictions, headers, bounds, escaping, startup errors, no mutation routes, public-package delegation.
- Browser check: real-store copy; memory/status/agent/handoff views; no sample fallback; desktop/mobile; copy/download; manual fresh-agent dry run.
- Benchmark Store p95 < 50 ms and HTTP p95 < 200 ms for 10 results on a 10⁴-row fixture; report measured output.
- Run `go test ./...`, `go vet ./...`, `gofmt`, and build with Go 1.27.
