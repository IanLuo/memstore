<!-- specs:locked:638dae0 2026-10-31 type=system-design -->

## Link contract
- **upstream** (this doc relies on): .agents/specs/memory-store.md
- **referrers** (must cite this when they change): .agents/dispatch/memstore-web-ui.md

# System design — the memory store: one table, projections, fused recall
## Components & ownership
- **Store** — owns the `memory` table and lifecycle; public reads remain `Search`, `Recent`, `Get`, and `Stats`.
- **Projections** — owns FTS (+ optional vectors). **Refuses:** being the source of truth.
- **Matchers** — owns recall fusion (lexical ∪ relation-walk ∪ optional vector). **Refuses:** writing.
- **HTTP server** — owns loopback listener, routing, argument validation, response encoding and security headers. Calls only `Search`, `Recent`, `Get`, and `Stats`; contains no SQLite access and adds no Store/query interface.
- **Web UI** — uses the exact operation semantics and arguments available to the CLI. Projects/tasks is a view over recent records and stored labels; agent retrieval shows exact Search results. No project-state inference or manual handoff composer.
- **CLI** — existing verbs unchanged; `serve` process wiring only.
- **Importer** — one-way ingest; does not modify existing rows.

## Data model
- `memory`: `id · ts · kind · scope · title · text · extra · importance · tier · status · deleted_at · parent · supersedes · found_by · hash · origin · origin_id · ref · fs_node`.
- Provenance — `origin` (native|imported|a caller's label) · `origin_id` (the source's id) · `ref` (commit sha) · `fs_node` (the source's node key). `Put` writes all four from its caller, defaulting an empty `origin` to `native` and storing an empty `origin_id`, `ref` or `fs_node` as NULL. `(origin, origin_id)` is unique whenever `origin_id` is not NULL, so the same `origin_id` coexists under two origins and a re-put of the same pair is refused with `ErrDuplicateOrigin`.
- **A schema change reaches an existing file only through a `schema_version` bump.** `Open` applies the schema, the provenance `ALTER` probes, the `fs_node` index and the provenance unique index only when the file's version is not current — that is what leaves an already-current file taking no write lock on open, which is what lets a reader race a writer. The `schema` statements are applied in one transaction, so a second opener sees the whole schema or none of it. An additive table or column added without a bump lands in a new file and nowhere else: bump `schema_version` when the shape changes, because the version check is what carries the change in. The current version is 6: v5 → 6 adds `idx_memory_scope_recent`; v4 → 5 adds the partial unique index over `(origin, origin_id)`; v3 → 4 was the porter index's rebuild. The unique index is built over the rows a file already carries, so `Open` fails loudly, naming it, when pre-existing rows hold a duplicate pair. This slice adds no schema change.
- Immutable: content — `id`, `ts`, `kind`, `scope`, `title`, `text`, `extra`, `importance`, `found_by`, `hash`, and the provenance columns. Mutable: `status`, `deleted_at` and `tier`, plus `parent` and `supersedes`, which a verb sets once.
- `record_fts`: FTS5 external-content over (`title`,`text`) — no copy of the text, `porter` tokenizer.
- `record_vec`: optional (`id`, `vector`); created only by `SetEmbedder`. `serve` never calls it, so a served store has no vector table and no vector matcher.
- `idx_memory_origin_origin_id`: UNIQUE over `(origin, origin_id)` WHERE `origin_id` IS NOT NULL.
- `idx_memory_scope_recent`: over `(scope, deleted_at, ts DESC, id DESC)`; `Recent`'s predicate and order in one index.
- **Web read boundary:** no Store method is added for the UI. The server maps routes to `Stats()`, `Recent(scope,k)`, `Search(q,k)`, and `Get(id)` only. Any behavior not expressible through those methods is not implemented in the UI.
- UI lists and labels are derived from returned `Memory` fields. Scope and memory pickers are populated only from an already-loaded `Recent` result; they are explicitly incomplete. Selecting a memory may seed text for a normal `Search` call. The UI must not infer project completion or create a project/task/status model.

## Boundaries & interfaces
- Verbs: `put · get · search · recent · link · close · forget(soft) · stats · import · serve`. `serve` starts a process and returns no memory data; the other verbs are unchanged.
- `serve` binds `127.0.0.1` only, defaulting to port 8765 with a port flag and no bind-address flag. The store path is `MEMSTORE_DB` or the home default; the browser cannot select a path.
- HTTP surface, all GET/HEAD, same origin: `/` (UI), `/api/stats`, `/api/search?q=&k=`, `/api/recent?scope=&k=`, `/api/get?id=`. Routes preserve the semantics of the matching CLI verb. Any other method is 405; no route writes.
- A handler translates the request into `Store.Search`, `Store.Recent`, `Store.Get` or `Store.Stats`, then encodes JSON. No SQL is constructed by the HTTP layer.
- Responses carry `Cache-Control: no-store`, a CSP with no external sources, `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`. A `Host` that is not loopback is refused before routing.
- The UI embeds its HTML in the web package, so the served page needs no external asset, font or network call.
- The UI stores no user state and has no handoff composer, checklist, or dry-run data.

## Failure modes
- Store open or version failure → `serve` exits with the Store error and serves nothing; no sample or stale fallback.
- Invalid/missing command arguments, non-positive k, or over-long query → 400 with a short message; the store is not touched.
- Store read failure → 500 with a generic message; the local diagnostic goes to stderr without memory text or query contents.
- Port occupied → clear startup error; the operator may pick another port.
- Non-loopback `Host`, or a cross-origin caller → refused; no CORS headers are ever sent.
- HTML or script markup in memory text → escaped by the UI and rendered as text; the CSP is the second line of defence.
- Large store or broad scope → results are bounded by the existing `k` argument; no endpoint returns an unbounded result.
- Search candidate versus actual prompt → the UI labels memstore results as candidates; prompt, context and provider cache remain unknown.
- Wrecking failure: silent loss of a claim → append-only content + soft delete + content hash.

## Scale assumptions
- 1 user, 1 writer; 10²–10⁴ memories now, growth ~10²/day.
- Open is O(1) — no replay; search is O(index); a scoped Recent listing is O(scope rows + log n), served by `idx_memory_scope_recent`. An all-scope Recent request may scan and sort; it uses the existing public `Recent` behavior.
- Recent and Search default to 10, matching existing CLI defaults. The operator may choose a larger `k`; every result still comes from the same `Recent` or `Search` call the CLI/API exposes. No pagination cursor or offset is added.
- Target: Store `search` p95 < 50 ms and HTTP p95 < 200 ms for `Search(q, 10)` at 10⁴ rows.
- Shard: one file per scope, opened lazily; cold rows tiered out of the hot index.
