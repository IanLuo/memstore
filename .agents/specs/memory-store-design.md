## Link contract
- **upstream** (this doc relies on): .agents/specs/memory-store.md
- **referrers** (must cite this when they change): .agents/dispatch/memstore-web-ui.md

# System design — the memory store: one table, projections, fused recall
## Components & ownership
- **Store** — owns the `memory` table and its lifecycle. **Refuses:** ranking, HTTP. Adds two public reads this slice needs: `Query` (text and/or exact filters, one bounded page) and `Relations` (a memory's stored edges).
- **Projections** — owns FTS (+ optional vectors). **Refuses:** being the source of truth.
- **Matchers** — owns recall fusion (lexical ∪ relation-walk ∪ optional vector). Applies the exact filters inside each matcher, before the limit, so ordering and truncation stay in one place. **Refuses:** writing.
- **CLI** — owns the verb surface. **Refuses:** business logic. `serve` is process wiring: parse the port, open the store, start the listener.
- **Web** — a separate package owning the loopback listener, routing, request validation, response bounds, JSON encoding, security headers and the embedded UI. Calls only exported Store methods; imports no SQLite driver, executes no SQL, and defines no second data layer.
- **Handoff builder** — part of the UI, running in the browser. Assembles operator-entered fields plus selected `Memory` values returned by the Store. Copy and Markdown download are explicit user actions. No handoff reaches the server or the database.
- **Importer** — owns one-way ingest. **Refuses:** modifying rows that already exist.

## Data model
- `memory`: `id · ts · kind · scope · title · text · extra · importance · tier · status · deleted_at · parent · supersedes · found_by · hash · origin · origin_id · ref · fs_node`.
- Provenance — `origin` (native|imported|a caller's label) · `origin_id` (the source's id) · `ref` (commit sha) · `fs_node` (the source's node key). `Put` writes all four from its caller, defaulting an empty `origin` to `native` and storing an empty `origin_id`, `ref` or `fs_node` as NULL. `(origin, origin_id)` is unique whenever `origin_id` is not NULL, so the same `origin_id` coexists under two origins and a re-put of the same pair is refused with `ErrDuplicateOrigin`.
- **A schema change reaches an existing file only through a `schema_version` bump.** `Open` applies the schema, the provenance `ALTER` probes, the `fs_node` index and the provenance unique index only when the file's version is not current — that is what leaves an already-current file taking no write lock on open, which is what lets a reader race a writer. The `schema` statements are applied in one transaction, so a second opener sees the whole schema or none of it. An additive table or column added without a bump lands in a new file and nowhere else: bump `schema_version` when the shape changes, because the version check is what carries the change in. The current version is 6: v5 → 6 adds `idx_memory_scope_recent`; v4 → 5 adds the partial unique index over `(origin, origin_id)`; v3 → 4 was the porter index's rebuild. The unique index is built over the rows a file already carries, so `Open` fails loudly, naming it, when pre-existing rows hold a duplicate pair. This slice adds no schema change.
- Immutable: content — `id`, `ts`, `kind`, `scope`, `title`, `text`, `extra`, `importance`, `found_by`, `hash`, and the provenance columns. Mutable: `status`, `deleted_at` and `tier`, plus `parent` and `supersedes`, which a verb sets once.
- `record_fts`: FTS5 external-content over (`title`,`text`) — no copy of the text, `porter` tokenizer.
- `record_vec`: optional (`id`, `vector`); created only by `SetEmbedder`. `serve` never calls it, so a served store has no vector table and no vector matcher.
- `idx_memory_origin_origin_id`: UNIQUE over `(origin, origin_id)` WHERE `origin_id` IS NOT NULL.
- `idx_memory_scope_recent`: over `(scope, deleted_at, ts DESC, id DESC)`; `Recent`'s predicate and order in one index.
- **Read API added by this slice:**
  - `Filter{Kind, Scope, Status, Origin string}` — exact values; an empty field means any. Filters combine with AND.
  - `Store.Query(q string, f Filter, limit, offset int) ([]Memory, bool, error)` — one bounded page plus a has-more flag. `q` empty lists newest first by `(ts DESC, id DESC)`; `q` non-empty ranks with the existing matchers. `limit` outside 1..`MaxPageSize`(100) or a negative offset is an error, never a silent clamp.
  - `Store.Relations(id int64) ([]Related, error)` — the stored outgoing edges a search hit would surface; a forgotten target is reported by id without inventing content.
- Pagination is offset-based and deterministic: rank order for a text query, `(ts DESC, id DESC)` otherwise. One extra row is fetched to answer has-more, so no count query is needed and a filtered page still fills. No cursor state is stored.
- Handoff packet fields: goal; repository, branch/commit, working-tree state; current state; decisions and constraints; source memories (exact text, id, scope, provenance, timestamp); evidence; risks and open questions; next action; fresh-agent instructions. Operator fields stay distinct from source memories, and empty checkout or evidence fields remain explicitly unverified.

## Boundaries & interfaces
- Verbs: `put · get · search · recent · link · close · forget(soft) · stats · import · serve`. `serve` starts a process and returns no memory data; the other verbs are unchanged.
- `serve` binds `127.0.0.1` only, defaulting to port 8765 with a port flag and no bind-address flag. The store path is `MEMSTORE_DB` or the home default; the browser cannot select a path.
- HTTP surface, all GET/HEAD, same origin: `/` (UI), `/api/stats`, `/api/memories` (query, filters, limit, offset), `/api/memories/{id}`. Any other method is 405; no route writes.
- A handler translates the request into `Store.Query`, `Store.Get`, `Store.Relations` or `Store.Stats`, then encodes JSON. Filter values are passed as parameters, never interpolated into SQL.
- Responses carry `Cache-Control: no-store`, a CSP with no external sources, `X-Content-Type-Options: nosniff` and `Referrer-Policy: no-referrer`. A `Host` that is not loopback is refused before routing.
- The UI embeds its HTML in the web package, so the served page needs no external asset, font or network call.
- Retention of packet drafts, checklist state and dry-run text is browser memory only; no endpoint accepts them and reload discards them.

## Failure modes
- Store open or version failure → `serve` exits with the Store error and serves nothing; no sample or stale fallback.
- Invalid filter value, limit, offset or over-long query → 400 with a short message; the store is not touched.
- Store read failure → 500 with a generic message; the local diagnostic goes to stderr without memory text or query contents.
- Port occupied → clear startup error; the operator may pick another port.
- Non-loopback `Host`, or a cross-origin caller → refused; no CORS headers are ever sent.
- HTML or script markup in memory text → escaped by the UI and rendered as text; the CSP is the second line of defence.
- Large store or broad filter → bounded page, one extra row for has-more; no endpoint returns an unbounded result.
- Forgotten relation target → reported as unavailable by id.
- Candidate versus actual prompt → the UI labels stored candidates separately from the unknown prompt, context and cache state; no number is invented for a value the store cannot measure.
- Stale or invented checkout state in a handoff → the fields are operator-entered and default to unverified, and the packet instructs the fresh agent to check the repository before editing.
- Wrecking failure: silent loss of a claim → append-only content + soft delete + content hash.

## Scale assumptions
- 1 user, 1 writer; 10²–10⁴ memories now, growth ~10²/day.
- Open is O(1) — no replay; search is O(index); a scope listing is O(scope rows + log n), served by `idx_memory_scope_recent`; a filter-only listing without a scope scans, which is acceptable at this size and measured before any index is added.
- Page size default 20, maximum 100; a 10⁴-row store needs at most 500 pages.
- Target: Store `search` p95 < 50 ms and HTTP p95 < 200 ms for 10 results at 10⁴ rows.
- Shard: one file per scope, opened lazily; cold rows tiered out of the hot index.
