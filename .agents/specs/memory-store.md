# Spec — a portable memory store: recall what matters, keep it small

## Problem & who breaks
- **Who:** one person across sessions and projects, plus the agents acting for them — single user, local, no server.
- **Break if nothing changes:** knowledge does not cross the session or repo boundary; recall is exact-token, so a query whose words are absent returns nothing; a fresh agent re-derives settled decisions and contradicts them.
- **Why now:** the current memory (fs) splits one fact across stores, doubles every payload in a search mirror, and leaves its largest part unqueryable.

## Non-negotiable outcome
- One SQLite file holds every memory; a fuzzy query returns the correct memory in the top 5 when the exact words are absent; 10× the memories leaves recall p95 within 1.5×.
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
- GIVEN an empty store WHEN `put` a memory THEN `get` returns it byte-identical.
- GIVEN D1 and D2 where `supersedes(D1)=D2` WHEN `search` finds D1 THEN the result carries D2's id.
- GIVEN a memory whose exact words are absent from a query but whose meaning matches WHEN `search q k=5` THEN it is in the top 5.
- GIVEN `forget(id)` WHEN `get(id)` THEN the row is hidden from reads and its bytes remain.
- GIVEN a 10× corpus WHEN `search` runs THEN p95 ≤ 1.5× baseline.
- Empty input: a `put` with empty `text` is rejected; the smallest accepted input is non-empty `text` + `kind`.

## KPIs & failure signal
- **Good:** top-5 recall ≥ target on a held-out query set; p95 `search` < 50 ms at 10⁴ memories; store bytes ≤ 2× text bytes.
- **Off-switch:** recall no better than `grep` on the same corpus — then the fused matcher was not worth its complexity.

## Non-functional requirements
- Performance: open < 50 ms; `search` p95 < 50 ms at 10⁴ memories; flat to 10×.
- Portability: one file, offline, no server.
- Size: bytes ≤ 2× the text — no payload copy in the index.
- Durability: content append-only; a crash loses nothing already written.
- Cost: no external service.

## Assumptions & dependencies
- Single user, local, no server. *Invalidated by:* multi-writer.
- SQLite with FTS5 present. *Invalidated by:* a store that must be remote.
- Blurry works with no model (lexical ∪ graph); vectors optional. *Invalidated by:* requiring semantic recall with no model.
- Import source is fs's store; designed-for, not blocking. *Invalidated by:* fs's schema changing shape.

## Data requirements
- **Inputs:** memories — `kind`, `scope`, `text`, optional `extra` and relations.
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
- NFRs → a benchmark at 1× and 10×, plus a size assertion.
- Retrieval quality → a held-out query set, scored in CI.
