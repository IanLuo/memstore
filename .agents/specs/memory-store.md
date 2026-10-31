## Link contract
- **upstream** (this doc relies on): none
- **referrers** (must cite this when they change): .agents/specs/memory-store-design.md, .agents/dispatch/memstore-web-ui.md

# Spec — a portable memory store: recall what matters, keep it small
## Problem & who breaks
- **Who:** one person across sessions and projects, plus the agents acting for them — single user, local, loopback only.
- **Break if nothing changes:** knowledge does not cross the session or repo boundary; exact-token recall misses absent words; the operator must read CLI JSON to see store state, and cannot see which memories memstore would return for a query. A returned candidate does not prove any agent received it.
- **Why now:** the operator needs to inspect real memories and store health locally, and to hand a task to an agent with no conversation history, without abandoning the one-file, package-owned data boundary.

## Non-negotiable outcome
- One SQLite file holds every memory; the retrieval goal remains the correct memory in the top 5 when exact words are absent; `search` p95 stays under 50 ms at 10⁴ memories (the 10× ratio is an observation, not a bound — see Non-functional requirements).
- A loopback-only read-only UI shows accurate live store facts, searches and filters real memories, and displays the exact candidates memstore returns for a query.
- The operator can build, inspect and export a handoff packet for a fresh agent with no conversation history. The packet separates exact source memories from operator-entered checkout state and evidence; unverified state is marked unverified.
- The UI never presents candidates as confirmed prompt contents, never invents model-context or LLM-cache numbers, and never writes memory data.
- Falsifiable over a fixed retrieval corpus and by comparing every UI value with the public Store API. This slice configures no embedder, so zero-overlap semantic recall is not promised.

## Scope in / out + primary flow
- **In:** one SQLite store; lexical ∪ relation-walk ∪ optional vector search; the CLI verbs; one-way fs import; `serve`, a loopback read-only web UI; live store status; text search with exact `kind`/`scope`/`status`/`origin` filters; filter-only listing; recent listing; memory detail with provenance and relations; agent-candidate view; fresh-agent handoff builder.
- **Search contract for the UI:** text keeps the existing Store behavior — FTS5 bm25 over porter-stemmed terms, OR-joined, plus vectors only when an embedder is configured by the caller. `serve` configures none, so it searches lexically. Exact filters combine with AND inside the package before the limit is applied, so a valid match is never lost to a post-filtered top-k. Filter-only listing is newest first (`ts DESC, id DESC`). Forgotten rows are always excluded. Zero shared stems may return nothing.
- **Pagination:** one page is at most 100 rows (default 20) at a non-negative offset. Order is deterministic: ranking order for text queries, `ts DESC, id DESC` for filter-only listing.
- **Handoff packet:** goal; repository, branch/commit and working-tree state; verified/unverified current state; decisions and constraints; selected source memories (exact text, id, scope, provenance, timestamp); evidence/tests; open questions and risks; next action; instructions telling the fresh agent to verify the checkout before editing. Checkout and evidence fields are operator input, never inferred from memory. The packet is labeled as carrying no conversation history.
- **Out (non-goals):**
  - LAN access, multi-user, accounts or auth — loopback only, one user.
  - Telemetry, logs and traces — none emitted; memory text and queries are never logged.
  - UI writes or lifecycle actions (`put`, `close`, `forget`, `link`, `import`) — read-only in this slice.
  - Claims about actual agent retrieval, prompt inclusion, context-window occupancy or provider prompt-cache use — no agent or provider integration exists.
  - Project/task progress or agent task ordering — memstore has no such data model; the UI shows store facts, and handoff state is operator-entered.
  - Persistence of handoff drafts, checklists or dry-run results — browser-only and transient.
  - External services, outbound network calls, external assets.
  - A CLI query language — structured filters are a package/API capability behind the UI, not new CLI verbs.
- **Primary flows:**
  - CLI: open file → `put` → `search` → `close` superseded memory → `import` old fs events.
  - UI: start the local server → inspect store facts → search text and/or combine filters → inspect a memory's text, provenance and relations → view the same query as agent candidates with a rough text-size estimate → fill and inspect a handoff packet, copy or download it → optionally run it through a genuinely fresh agent and compare the answer with the packet.

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
- GIVEN `serve` with no port flag WHEN started THEN it opens the configured store through the package, binds only `127.0.0.1:8765`, and prints the local URL. A port flag is allowed; no bind-address flag exists. A bind or open failure exits with a clear error and serves nothing.
- GIVEN a store WHEN its overview is requested THEN returned counts, database and WAL bytes and timestamp bounds equal `Store.Stats()`. `Stats.Open` is labelled live/not-forgotten rows, not rows whose `status` is `open`. No schema version is reported, because the package does not expose it.
- GIVEN a text query with any combination of exact kind, scope, status and origin WHEN queried THEN every returned row satisfies all filters and the rows keep Store search order. A row that matches a filter but ranks below an unfiltered page boundary is still returned when it is inside the filtered page — filtering happens before limiting.
- GIVEN no text with one or more filters WHEN queried THEN live matching rows come back newest first. GIVEN no text and no filters THEN a bounded recent page comes back. Forgotten rows never appear.
- GIVEN more rows than one page WHEN the next offset is requested THEN the next page is deterministic and non-overlapping; a limit above 100 or a negative offset is refused rather than clamped.
- GIVEN a memory WHEN its detail is requested THEN the stored text, provenance, lifecycle fields and its stored relations are returned; a relation whose target is forgotten is reported as unavailable, never invented.
- GIVEN the agent view WHEN a query returns matches THEN it shows those exact rows in that order. A size figure is labelled an estimate and states its method (bytes of returned text ÷ 4). Prompt inclusion, model context and LLM cache use are labelled unknown/not connected.
- GIVEN the status view WHEN rendered THEN live Store statistics are visually distinct from static capability notes, and the view states that no embedder is configured.
- GIVEN the handoff view WHEN the packet is previewed or exported THEN it contains goal, repository and checkout, current state, decisions and constraints, selected source memories with ids and provenance, evidence, risks, next action and fresh-agent instructions. Blank checkout or evidence fields are marked not verified/not provided, never inferred.
- GIVEN source memories are selected WHEN the packet is built THEN their exact text and identifiers are included, and source memory text is visually distinct from operator-entered text.
- GIVEN a copied fresh-agent response or review note WHEN the packet is exported THEN neither appears in the export; both stay in the browser, and no server endpoint accepts them.
- GIVEN the readiness checklist or dry-run prompt WHEN used THEN the UI labels it a completeness aid, not proof that an agent understands or will succeed.
- GIVEN any HTTP request WHEN handled THEN UI and API reads call only public methods of the Go memstore package; HTTP code opens no SQLite handle, executes no SQL and defines no parallel database interface. No write route exists and non-read methods are refused.
- GIVEN memory text containing HTML or script markup WHEN rendered THEN it is displayed as text and does not execute.
- GIVEN an invalid filter value, an over-long query, an out-of-range page or an unreadable store WHEN requested THEN a clear error is returned and no sample or stale data is presented as live.
- **Smallest valid UI request:** no query and no filters → a bounded recent page. **Smallest rejected request:** a limit above 100, a negative offset, or an unknown query parameter value outside the accepted set.

## KPIs & failure signal
- **Good:** top-5 recall meets the held-out target on the fixed corpus; Store `search` p95 < 50 ms at 10⁴ memories; the text is stored once. On a copy of the real store the operator can find a known memory with text plus combined filters, browse a filter-only set, and tell memstore candidates apart from actual prompt contents. A fresh agent given only the exported packet can restate the goal, the verified-versus-unverified state, the constraints and the next action.
- **Size:** measured db/text ≈4.36× at 10³ and ≈3.57× at 10⁴ on `cmd/bench`'s synthetic small-row corpus (~84 B per memory); no bulk-payload ratio is measured.
- **API target:** local HTTP p95 < 200 ms for a 10-result query on a 10⁴-row fixture. Requires recorded benchmark output.
- **Off-switches:** recall no better than `grep` on the same corpus — then the fused matcher was not worth its complexity; do not ship the agent view if it cannot show exact Store results while keeping candidate and prompt distinct; do not ship combined filtering if it changes or hides Store ranking; a checklist score alone is never evidence that a handoff works.

## Non-functional requirements
- Performance: open < 50 ms; Store `search` p95 < 50 ms at 10⁴ memories; local HTTP p95 < 200 ms for a 10-result query at 10⁴ rows. A scope listing stays one indexed SEARCH with no temp b-tree. No new index is added without a measurement that justifies it.
- Portability: one file, offline, local user; loopback bind only; no external services, assets, CORS, outbound calls or telemetry.
- HTTP safety: GET/HEAD only; other methods 405; same origin; non-loopback `Host` values refused; memory text escaped and rendered as text; Content-Security-Policy with no external sources; `Cache-Control: no-store` on memory responses; bounded query length and page size; memory text and queries never logged.
- Durability: append-only content and the existing crash guarantee are unchanged; the UI and server write no memory rows and no schema migration is added.
- Privacy: handoff drafts, checklist state, dry-run responses and review notes stay in the browser; only an explicit copy or download leaves it. No provider is contacted.
- Cost: no external service.

## Assumptions & dependencies
- One user, one local file, one writer at a time; several processes may open the file and wait for the lock. The UI server is a read-only opener. *Invalidated by:* multi-writer or remote access — reopen security and store-selection design.
- The store is `MEMSTORE_DB`, else `~/.memstore/memory.db`; the browser cannot choose a path. `serve` defaults to port 8765 and accepts a port override only. *Invalidated by:* the port being taken — use the override.
- SQLite with FTS5 and the Go Store API remain available; an open failure stops the server and serves nothing. *Invalidated by:* a store that must be remote.
- Only public package methods may read data; new package read methods are allowed, HTTP SQL is not. *Invalidated by:* a filtered ranking that cannot be expressed in the package — revise the package boundary first.
- `serve` configures no embedder; zero-overlap semantic search is not provided. *Invalidated by:* an approved model integration with defined cache behavior.
- No agent or provider telemetry exists, so a candidate is never proof of retrieval. *Invalidated by:* an integration that reports actual retrieval and provider cache use.
- fs remains the import source; a change to its schema triggers review. *Invalidated by:* fs changing shape.

## Data requirements
- **Inputs:** memories — `kind`, `scope`, `text`, optional `extra`, provenance (`origin`, `origin_id`, `ref`, `fs_node`) and relations. A UI request carries an optional query, exact `kind`/`scope`/`status`/`origin`, a limit and an offset; the store path comes from the environment or the home default.
- **Outputs:** ranked search results, filter-only and recent listings, one memory with its relations, paging metadata, `Stats()`. Handoff packet fields as listed in the scope. No schema version or filter vocabulary is claimed unless a public method returns it.
- **Must survive:** claims (decisions, knowledge, goals) forever; content immutable. The UI persists nothing; handoff state is transient in the browser and explicit export is the only persistence action. HTTP memory responses are not cached.
- **Empty record/request:** an empty `put` text is rejected; unknown `kind` is accepted as an open label. Empty UI text is valid — with filters it lists matching rows, without filters it returns a bounded recent page. A UI request cannot write.

## Rollback & recovery
- Fail-closed on write (no partial memory); fail-open on read (fewer results, never wrong). `serve` fails closed at startup when the store cannot be opened, and never substitutes sample or stale data.
- **Reverted means:** stop and remove the server command, the web package and the added package read methods. No schema migration and no memory row changes to reverse; the CLI keeps working.
- **One wrecking failure:** silent loss of a claim — mitigated by append-only content, soft delete and a content hash. The UI-specific wrecking failure is presenting a candidate as retrieved context, or mutating a claim from a read-only surface; mitigated by explicit labels and the absence of any write route.

## Security & compliance
- Store local, user-only permissions; no secrets in memory by policy (redact at write); no regulatory obligations apply.
- `forget` is soft and one-way; no verb destroys bytes, and the UI exposes no destructive action, so no confirmation flow is needed.
- The server binds IPv4 loopback only, refuses non-loopback `Host`, sends no CORS headers and makes no outbound request. Other processes on the same machine can read the store through the loopback endpoint; that residual risk is accepted for a single-user local tool.

## Verification plan
- Store acceptance → automated fixture tests; provenance, version and index cases have dedicated tests; the scope-listing test asserts an indexed SEARCH with no temp b-tree.
- New package reads → tests for AND semantics across all four filters, preserved BM25 order after filtering, a match beyond the unfiltered page boundary still returned inside the filtered page, filter-only newest-first order, forgotten exclusion, deterministic non-overlapping pages, refused limit/offset, relations and stats.
- HTTP → tests for loopback binding and port override, startup failure, allowed methods, non-loopback `Host` refusal, security and cache headers, query/page bounds, escaped markup, public-package delegation, absence of write routes, and empty/error responses. Review confirms no SQL or SQLite handle in HTTP code.
- Browser → desktop and mobile checks against a copy of the real store: combined search, filter-only listing, detail and relations, live stats versus static notes, exact agent candidates with estimate/unknown labels, handoff build and export, blank-field warnings, and one dry run in a fresh agent whose answer is compared with the packet.
- NFRs → benchmark Store p95 < 50 ms and HTTP p95 < 200 ms for 10 results at 10⁴ rows, recording the command, fixture and output.
- Retrieval quality → held-out query set; the UI claims no recall improvement.
