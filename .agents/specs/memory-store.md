<!-- specs:locked:9586fa7 2026-10-16 type=spec -->

## Link contract
- **upstream** (this doc relies on): none
- **referrers** (must cite this when they change): .agents/specs/memory-store-design.md

# Spec — a portable memory store: recall what matters, keep it small
## Problem & who breaks
- **Who:** one person across sessions and projects, plus the agents acting for them — single user, local, no server.
- **Break if nothing changes:** knowledge does not cross the session or repo boundary; recall is exact-token, so a query whose words are absent returns nothing; a fresh agent re-derives settled decisions and contradicts them.
- **Why now:** the current memory (fs) splits one fact across stores, doubles every payload in a search mirror, and leaves its largest part unqueryable.

## Non-negotiable outcome
- One SQLite file holds every memory; a fuzzy query returns the correct memory in the top 5 when the exact words are absent; `search` p95 stays under 50 ms at 10⁴ memories (the 10× ratio is a measured observation, not a bound — see Non-functional requirements).
- Falsifiable by a tester over a fixed corpus with no exact-term match.

## Scope in / out + primary flow
- **In:** one store (one table); search — lexical ∪ relation-walk ∪ optional vector; the verb interface; a CLI; one-way import of fs's events.
- **Out (non-goals):**
  - a server or network access — a memory is a file.
  - multi-user or auth — one user.
  - logs and traces — telemetry, not memory; a file at most.
  - a UI — CLI only.
  - a query language — the verbs are the surface.
- **Primary flow:** open file → `put` a memory → `search` by meaning → `close` a superseded memory → `import` old events.

## Acceptance criteria
- GIVEN an empty store WHEN `put` a memory THEN `get` returns it with byte-identical text and an equal timestamp (the zone normalises to UTC; the instant is preserved at nanosecond resolution).
- GIVEN a `put` carrying `origin`, `origin_id`, `ref` and `fs_node` WHEN `get` THEN the four fields round-trip; an empty `origin_id`, `ref` or `fs_node` is stored NULL and an empty `origin` becomes `native`. GIVEN a memory with `(origin, origin_id)` WHEN `put` the same pair again THEN it is refused with `ErrDuplicateOrigin` and the file still holds one row; the same `origin_id` under a different `origin` is accepted. GIVEN a `schema_version`-4 file WHEN opened THEN it reports 5, carries the partial unique index, and keeps its rows; a file with pre-existing duplicate `('imported', id)` rows fails `Open` loudly, naming the index.
- GIVEN D1 and D2 where `supersedes(D1)=D2` WHEN `search` finds D1 THEN the result carries D2's id.
- GIVEN a memory and a query that shares at least one stemmed term with it — an inflected form, or one token of several — WHEN `search q k=5` THEN the memory is in the top 5, best first; with no shared stem and no embedder configured, the store returns fewer results, never a wrong one.
- GIVEN `forget(id)` WHEN `get(id)` THEN the row is hidden from reads and its bytes remain.
- GIVEN a 10× corpus WHEN `search` runs THEN p95 < 50 ms at 10⁴ memories (the absolute bound; the 10× ratio is a measured observation, not a bound — see Non-functional requirements).
- GIVEN a store file that does not exist WHEN two processes open it and write at once THEN both succeed and every write is present.
- Empty input: a `put` with empty `text` is rejected; the smallest accepted input is non-empty `text` + `kind`.

## KPIs & failure signal
- **Good:** top-5 recall ≥ target on a held-out query set; p95 `search` < 50 ms at 10⁴ memories; size — the text is stored once; measured db/text ≈4.36× @10³ and ≈3.57× @10⁴ on `cmd/bench`'s synthetic small-row corpus (~84 B per memory), where the overhead is the index and the row columns. **No bulk-payload ratio is measured:** the ≈1.3× that stood here had no measurement behind it and is withdrawn rather than restated.
- **Off-switch:** recall no better than `grep` on the same corpus — then the fused matcher was not worth its complexity.

## Non-functional requirements
- Performance: open < 50 ms; `search` p95 < 50 ms at 10⁴ memories. The 10× p95 ratio is a measured observation, not a bound: 1.786× with the AND query at `5ec1768` (p95 0.393 ms), 3.778× with the OR query at `b3e5e0c` (p95 1.664 ms). OR over the query's stems saturates hits per query at k=10, so bm25 scores a candidate set that grows with N, where the AND query returned almost nothing. **Evidence, per revision** — the AND-era figures are `memstore:t-d2c7acc3` (the probe run at `d735e3f`, which measured 1.867× and 0.423 ms); the OR figures and the cause are the independent review of `b3e5e0c`, `cap:t-ab50a7ad` (worker `memstore:t-4db2eb42`).
- Portability: one file, offline, no server.
- Size: the text is stored once — FTS is external-content, never a copy; measured total bytes ≈4.36× at 10³ and ≈3.57× at 10⁴ on the small-row corpus (the overhead is the index and the row columns). **No bulk-payload ratio is measured;** the ≈1.3× figure that stood here is withdrawn, and the same false attribution was corrected in the KPIs above.
- Durability: content append-only; a crash loses nothing already written.
- Concurrency: several processes may open the store, including the first creation of the file; a caller waits for the lock rather than failing, and no write is lost.
- Cost: no external service.

## Assumptions & dependencies
- One writer at a time: single user, local, no server, and several processes may open the file — each waits its turn rather than failing. *Invalidated by:* multi-writer.
- SQLite with FTS5 present. *Invalidated by:* a store that must be remote.
- Blurry works with no model (lexical ∪ graph); vectors optional. *Invalidated by:* requiring semantic recall with no model.
- Import source is fs's store; designed-for, not blocking. *Invalidated by:* fs's schema changing shape.

## Data requirements
- **Inputs:** memories — `kind`, `scope`, `text`, optional `extra`, provenance (`origin`, `origin_id`, `ref`, `fs_node`) and relations.
- **Outputs:** ranked list (`search`), one by id (`get`), recent list, `stats`.
- **Must survive:** claims (decisions, knowledge, goals) forever; content immutable.
- Empty record: rejected (empty `text`); unknown `kind` accepted (open label).

## Rollback & recovery
- Fail-closed on write (no partial memory); fail-open on read (fewer results, never wrong).
- **Reverted means:** content rows deleted; projections rebuilt from the source rows.
- **One wrecking failure:** silent loss of a claim — mitigated by append-only content, soft delete, and a content hash.

## Security & compliance
- Store local, user-only permissions; no secrets in memory by policy (redact at write).
- No regulatory obligations apply.
- `forget` is soft and reversible; no verb destroys bytes.

## Verification plan
- Each acceptance bullet → an automated test over a fixture corpus.
- The provenance contract → round-trip, NULL, duplicate, coexistence, barrier and v4-upgrade tests; the index's refusal over pre-existing duplicates is asserted as a returned error naming the index, not a crash.
- NFRs → a benchmark at 1× and 10×, plus a measured size ratio.
- Retrieval quality → a held-out query set, scored in CI.
