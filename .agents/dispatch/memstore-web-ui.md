# Dispatch — command-faithful memstore status UI

## Contract
- Upstream: `.agents/specs/memory-store.md`, `.agents/specs/memory-store-design.md`.
- Implement only after both contracts are relocked for this slice.
- Do not touch the other untracked dispatch cards or `.playwright-mcp/` artifacts.

## Outcome
- `memstore serve` stays read-only on `127.0.0.1:8765` by default; optional port override, no bind-address override.
- UI shows four views: Projects & tasks (recent records by scope), Agent retrieval (exact `Search(q,k)` results), Memory search (same `Search` operation), and Store status (`Stats`).
- Full memory details use `Get(id)`. No handoff composer, project status inference, or agent-context simulation.
- Search and Recent default to k=10 in the web controls. The selected k is passed to the existing method; do not add a page cap that the CLI does not have.

## Boundaries
- Allowed package calls only: `Stats`, `Recent`, `Search`, `Get`. No `Query`, `Filter`, `Relations`, new CLI verbs, direct SQL, or other Store API added for this UI.
- Controls map to command arguments: recent scope/k; search query/k; get id; stats no args. Display kind/scope/status as returned, without deriving task lifecycle or aggregate health.
- Browser input cannot select a database path. Use `MEMSTORE_DB` or the existing home default.
- Host allowlist accepts loopback hosts only; GET/HEAD only; same origin; restrictive CSP; no-store responses; bounded inputs; no content/query logging.
- Agent view says results are candidates from this exact Search call; it does not say the agent received them. Prompt/context/cache are unavailable.

## Verification
- HTTP tests compare each response with the same direct public Store method and cover bad args, Host/method restrictions, headers, and escaping.
- Browser check against real-store copy: recent by scope and all scopes; Search ordering; Get text; stats; project/task records show stored labels; no handoff view.
- Run `go test ./...`, `go vet ./...`, `gofmt`, and build with Go 1.27.
