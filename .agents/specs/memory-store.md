<!-- specs:locked:b752b61 2026-11-01 type=spec -->

## Link contract
- **upstream** (this doc relies on): none
- **referrers** (must cite this when they change): .agents/specs/memory-store-design.md, .agents/dispatch/memstore-web-ui.md

# Spec — a portable memory store: recall what matters, keep it small
## Problem & who breaks
- **Who:** one person across sessions and projects, plus the agents acting for them — single user, local, loopback only.
- **Break if nothing changes:** knowledge does not cross the session or repo boundary; exact-token recall misses absent words; the operator must read CLI JSON to see store state and has no concise view of the records available to a fresh agent through normal memstore commands.
- **Why now:** the operator needs to inspect real memories, scopes, stored statuses, and exact search results locally, without abandoning the one-file, package-owned data boundary.

## Non-negotiable outcome
- One SQLite file holds every memory; the retrieval goal remains the correct memory in the top 5 when exact words are absent; `search` p95 stays under 50 ms at 10⁴ memories (the 10× ratio is an observation, not a bound; see Non-functional requirements).
- A loopback-only, read-only UI shows live store statistics, recent records by scope, exact Search results, and details via the same public operations a memstore CLI caller can use: `stats`, `recent`, `search`, and `get`.
- The UI shows stored records and their actual `kind`, `scope`, and `status`; it does not invent project health, task completion, or agent order.
- The UI never presents memstore results as an agent’s actual prompt/context and never invents model-context or LLM-cache measurements.
- Falsifiable by comparing every displayed record and statistic with the corresponding public Store operation. V1 configures no embedder; zero-overlap semantic recall is not promised.

## Scope in / out + primary flow
- **In:** one SQLite store; lexical ∪ relation-walk ∪ optional vector search; the CLI verbs plus `serve`; loopback read-only UI; `stats` overview; recent records via `Recent(scope, k)`; text search via `Search(q, k)`; one record via `Get(id)`; a projects/tasks view composed from recent stored records and their actual labels; an agent-retrieval view showing exact Search results and rank.
- **Selection:** visible scope and memory options come only from `Recent` rows already loaded into the UI. The UI labels the scope choices as incomplete; no all-scope catalogue exists. A selected memory can seed a Search query using its stored title or first text line; the UI still calls ordinary `Search(q,k)` and shows its exact results.
- **Interface boundary:** the server/UI uses only existing public memstore reads: `Stats`, `Recent`, `Search`, and `Get`. No new query, filter, relation, listing, or database interface is added for the UI. The handler does not query SQLite. UI controls must map directly to the arguments those existing operations accept.
- **Primary UI flow:** start server → inspect live `stats` → browse `recent` records in a scope → open a record through `get` → run a likely fresh-agent query through `search` → compare exact results with stored scope records → decide what instruction or follow-up is needed.
- **Out (non-goals):**
  - LAN access, multi-user, accounts or auth — loopback only, one user.
  - UI writes or lifecycle actions (`put`, `close`, `forget`, `link`) — read-only.
  - Combined structured filters that the CLI/store API does not expose — do not add them just for the UI.
  - A handoff composer, copied packet, or agent-context simulator — the useful “handoff view” is the stored context returned by normal memstore reads, not a manually authored summary.
  - Claims about actual agent retrieval, prompt inclusion, context-window occupancy, provider cache, task progress, or project health not stored in memstore — no such integration/model exists.
  - Telemetry, external services, outbound network calls, external assets.
- **Primary flow:** open file → `put` records → agent or operator uses `search`/`recent`/`get` → UI shows those same records and live stats so the operator can see what the store can provide and what is missing.

## Acceptance criteria
- GIVEN an empty store WHEN `put` a memory THEN `get` returns it with byte-identical text and an equal timestamp (zone normalises to UTC; the instant is preserved at nanosecond resolution).
- GIVEN a `put` carrying `origin`, `origin_id`, `ref` and `fs_node` WHEN `get` THEN the four fields round-trip; an empty `origin_id`, `ref` or `fs_node` stores NULL and an empty `origin` becomes `native`. GIVEN a duplicate `(origin, origin_id)` WHEN put again THEN `ErrDuplicateOrigin` and one row remains; the same `origin_id` under another origin is accepted. GIVEN a v4 file WHEN opened THEN it reports 6, carries the partial unique index, and keeps its rows; pre-existing duplicate pairs fail `Open` loudly, naming the index.
- GIVEN a store holding rows in more than one scope WHEN `recent` lists one scope THEN SQLite answers from `idx_memory_scope_recent` with a SEARCH and no temp b-tree, newest first. GIVEN a v5 file WHEN opened THEN it reports 6, carries the index, and keeps its rows; a file that cannot take the index fails `Open` loudly, naming it.
- GIVEN D1 and D2 where `supersedes(D1)=D2` WHEN `search` finds D1 THEN the result carries D2's id.
- GIVEN a memory and a query that shares at least one stemmed term with it — an inflected form, or one token of several — WHEN `search q k=5` THEN the memory is in the top 5, best first; with no shared stem and no embedder configured, the store returns fewer results, never a wrong one.
- GIVEN `forget(id)` WHEN `get(id)` THEN the row is hidden from reads and its bytes remain.
- GIVEN a 10× corpus WHEN `search` runs THEN p95 < 50 ms at 10⁴ memories (the absolute bound; the 10× ratio is an observation).
- GIVEN a store file that does not exist WHEN two processes open it and write at once THEN both succeed and every write is present.
- Empty input: a `put` with empty `text` is rejected; the smallest accepted input is non-empty `text` + `kind`.
- GIVEN `serve` with no port flag WHEN started THEN it opens the configured store through memstore, binds only `127.0.0.1:8765`, and prints the local URL. A port flag is allowed; no bind-address flag exists. A bind or open failure exits clearly and serves nothing.
- GIVEN Store status WHEN loaded THEN the shown counts, DB/WAL bytes and timestamp bounds equal `Stats()`. `Stats.Open` is labeled live/not-forgotten, not status=`open`.
- GIVEN a scope and result limit WHEN the project/task view loads THEN its records and order match `Recent(scope, k)` exactly. Empty scope maps to `Recent("", k)`; kind/status labels are displayed as stored and are not interpreted as task completion.
- GIVEN a query and result limit WHEN Agent retrieval or Memory search runs THEN its records, text and order match `Search(q, k)` exactly. The UI states candidates are not proof that an agent received them, and prompt/context/cache remain unknown.
- GIVEN a record id WHEN Read full memory is selected THEN its text and fields match `Get(id)`. HTML/script in memory text is rendered as text, not executed.
- GIVEN any UI request WHEN handled THEN the server calls only `Stats`, `Recent`, `Search`, or `Get`; it adds no Store API, command, SQL, SQLite access, hidden filter, or alternate ordering. No write route exists; non-read methods are refused.
- GIVEN invalid/missing arguments, an overlong query, or an unreadable store WHEN requested THEN a clear error is shown and no sample/stale result is substituted.
- **Smallest valid UI requests:** `stats`; `recent -k 1` with optional scope; `search -k 1 QUERY` for non-empty query; `get ID` for a live ID. Missing/invalid `k`, empty search query, or invalid ID are rejected consistently with the underlying CLI semantics.

## KPIs & failure signal
- **Good:** top-5 recall meets the held-out target on the fixed corpus; Store `search` p95 < 50 ms at 10⁴ memories; the text is stored once. On a copy of the real store the operator can find a known memory using normal `search`, browse the same scoped recent records available to an agent, and distinguish candidates from actual prompt contents.
- **Size:** measured db/text ≈4.36× at 10³ and ≈3.57× at 10⁴ on `cmd/bench`'s synthetic small-row corpus (~84 B per memory); no bulk-payload ratio is measured.
- **API target:** local HTTP p95 < 200 ms for `Search(q, 10)` on a 10⁴-row fixture. Requires recorded benchmark output.
- **Off-switches:** recall no better than `grep` on the same corpus — then the fused matcher was not worth its complexity; do not ship a view that implies a capability or state that the existing memstore reads do not provide.

## Non-functional requirements
- Performance: open < 50 ms; Store `search` p95 < 50 ms at 10⁴ memories; local HTTP p95 < 200 ms for `Search(q, 10)` at 10⁴ rows. A scope listing stays one indexed SEARCH with no temp b-tree. No new index is added without a measurement that justifies it.
- Portability: one file, offline, local user; loopback bind only; no external services, assets, CORS, outbound calls or telemetry.
- HTTP safety: GET/HEAD only; other methods 405; same origin; non-loopback `Host` values refused; memory text escaped and rendered as text; Content-Security-Policy with no external sources; `Cache-Control: no-store` on memory responses; query length is bounded; `k` follows existing CLI behavior and must be positive; memory text and queries never logged.
- Durability: append-only content and the existing crash guarantee are unchanged; the UI and server write no memory rows and no schema migration is added.
- Privacy: the UI sends only local read requests and does not collect agent or provider data. No provider is contacted.
- Cost: no external service.

## Assumptions & dependencies
- One user, one local file, one writer at a time; several processes may open the file and wait for the lock. The UI server is a read-only opener. *Invalidated by:* multi-writer or remote access — reopen security and store-selection design.
- The store is `MEMSTORE_DB`, else `~/.memstore/memory.db`; the browser cannot choose a path. `serve` defaults to port 8765 and accepts a port override only. *Invalidated by:* the port being taken — use the override.
- SQLite with FTS5 and the Go Store API remain available; an open failure stops the server and serves nothing. *Invalidated by:* a store that must be remote.
- Only existing public package reads (`Stats`, `Recent`, `Search`, `Get`) may read data for the UI. No UI-driven API additions. *Invalidated by:* an explicit user decision to change the CLI/package surface.
- `serve` configures no embedder; zero-overlap semantic search is not provided. *Invalidated by:* an approved model integration with defined cache behavior.
- No agent or provider telemetry exists, so a candidate is never proof of retrieval. *Invalidated by:* an integration that reports actual retrieval and provider cache use.
- Nothing else writes this store: fs's projection that once did was deleted, so the file has one writer, `put`. *Invalidated by:* a second writer appearing — reopen the concurrency and provenance contract.

## Data requirements
- **Inputs:** memories — `kind`, `scope`, `text`, optional `extra`, provenance (`origin`, `origin_id`, `ref`, `fs_node`) and relations. UI requests mirror the existing command arguments: `recent` scope and k; `search` non-empty query and k; `get` id; `stats` no arguments. Store path comes from environment or home default.
- **Outputs:** the exact results of `Search`, `Recent`, `Get`, and `Stats`. No filter vocabulary, task model, prompt state, or status inference is added.
- **Must survive:** claims (decisions, knowledge, goals) forever; content immutable. The UI persists nothing. HTTP memory responses are not cached.
- **Empty record/request:** an empty `put` text is rejected; unknown `kind` is accepted as an open label. Empty search text is rejected like the CLI; empty recent scope means all scopes. A UI request cannot write.

## Rollback & recovery
- Fail-closed on write (no partial memory); fail-open on read (fewer results, never wrong). `serve` fails closed at startup when the store cannot be opened, and never substitutes sample or stale data.
- **Reverted means:** stop and remove the server command and web package, and revert UI documentation. No schema migration or memory-row changes to reverse; existing CLI/package operations remain unchanged.
- **One wrecking failure:** silent loss of a claim — mitigated by append-only content, soft delete and a content hash. The UI-specific wrecking failure is presenting a candidate as retrieved context, or mutating a claim from a read-only surface; mitigated by explicit labels and the absence of any write route.

## Security & compliance
- Store local, user-only permissions; no secrets in memory by policy (redact at write); no regulatory obligations apply.
- `forget` is soft and one-way; no verb destroys bytes, and the UI exposes no destructive action, so no confirmation flow is needed.
- The server binds IPv4 loopback only, refuses non-loopback `Host`, sends no CORS headers and makes no outbound request. Other processes on the same machine can read the store through the loopback endpoint; that residual risk is accepted for a single-user local tool.

## Verification plan
- Store acceptance → automated fixture tests; provenance, version and index cases have dedicated tests; the scope-listing test asserts an indexed SEARCH with no temp b-tree.
- Existing public reads → HTTP tests compare `stats`, `recent`, `search`, and `get` responses with direct calls to those same Store methods.
- HTTP → tests for loopback binding and port override, startup failure, allowed methods, non-loopback `Host` refusal, security and cache headers, CLI argument bounds, escaped markup, public-method delegation, absence of write routes, and empty/error responses. Review confirms no SQL or SQLite handle in HTTP code.
- Browser → desktop and mobile checks against a copy of the real store: `Stats` values, `Recent` scope results/order, `Search` results/order, `Get` text, and explicit unavailable-agent-context labels.
- NFRs → benchmark Store p95 < 50 ms and HTTP p95 < 200 ms for a 10-result Search at 10⁴ rows, recording the command, fixture and output.
- Retrieval quality → held-out query set; the UI claims no recall improvement.
