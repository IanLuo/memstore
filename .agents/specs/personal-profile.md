# Spec — the personal profile: a bounded digest of current factors

Relies on `.agents/specs/memory-store.md` for the store's guarantees — immutable
content, one writer, soft forget, and no inference. This doc does not restate them.

## Problem & who breaks
- **Who:** a fresh agent session at cold start — primary; the operator, retyping a standing instruction in a new session — secondary.
- **Break if nothing changes:** `memory` is a version log. "Current" is not a fact the store holds: `supersedes` is written only by `close old new`, the `parent` column is written by no verb, and ten live rows for one factor is legal. A standing fact is found only by wording, ranked by bm25 against every event, and outranks nothing because `importance` is never set. So a session that must act on what recurs, or on what was settled, decides without that state.
- **Why now:** on-demand reads are cheap (50 ms), but nothing in the store states what is current or what repeats. Every reader would have to reconstruct it.

## Non-negotiable outcome
- One read returns a **bounded digest of current state**: at most 64 factors and at most 4 KB, one row per factor, computed from `memory` alone, stating no verdict.
- Falsifiable without argument: put 500 factors, read once → at most 64 rows and at most 4 KB, with an exact `omitted` count. Put three versions of one factor, read once → one row.
- Not this: a summary that loses detail, an inference engine, or a second source of truth.
- The digest is a projection of `memory`. It is never authoritative, never written, and cannot go stale, because it does not persist.

## Scope in / out + primary flow
- **In:** one read-only CLI verb `profile`; the store method behind it; the grouped read over `memory`; the digest row; the AGENTS.md line that tells an agent to read it on demand.
- **Out (non-goals), each kept out on purpose:**
  - Read-time verdicts ("recurring", "stale", "abandoned") — the store states no health, completion, or status inference (`memory-store.md`); analysis belongs to the reader.
  - A persisted profile table — no evidence yet that the read misses the bound.
  - Trend windows, cross-factor correlation, prose summaries — unbounded, and each one grows the digest past the size it exists to meet.
  - Any LLM or network call — no model is in the write path, and none may be.
  - Redaction of stored text — a guessing rule corrupts data on the way out.
  - A UI route, telemetry, a new `memory` column, hand-pinned entries, or a `schema_version` bump.
- **Primary flow:** session works → a decision depends on current state → runs `memstore profile -k 8` → reads at most 8 rows → acts. Never at start, never cached. A digest held in an agent's head is the failure this flow removes.
- **Interface:** `profile [-scope a,b] [-k 8]`. `-scope` filters which rows are counted (absent means all scopes); `-k` bounds how many rows return, default 8, capped at 64, `≤ 0` returns an empty list, an over-cap value clamps.

## Acceptance criteria
- **Resolution.** GIVEN three live rows sharing one `(kind, title)` WHEN `profile` runs THEN one row appears with count 3, `first` = earliest, `last` = latest, and the value of the newest open row.
- **Retired version.** GIVEN a newer closed row and an older open row under one `(kind, title)` WHEN `profile` runs THEN the value is the older open row's text, not the closed one.
- **All closed.** GIVEN a factor whose rows are all closed WHEN `profile` runs THEN it appears with its count and no value.
- **Not a factor.** GIVEN a row whose title is empty or only spaces, or a forgotten row WHEN `profile` runs THEN it is absent and adds to no count.
- **Title rule.** GIVEN `Build timeout` and `  build TIMEOUT  ` THEN one factor. GIVEN `build timeout` and `build-timeout` THEN two factors.
- **Scopes.** GIVEN one title in scopes `a` and `b` WHEN `profile -scope a` runs THEN count is that of `a` alone and `scopes` is 1; WHEN `profile -scope a,b` runs THEN the counts merge and `scopes` is 2.
- **Bound.** GIVEN 500 factors WHEN `profile` runs THEN ≤ 64 rows and ≤ 4 KB, and `omitted` states the exact number cut.
- **Order.** GIVEN two factors where one was seen later WHEN `profile -k 1` runs THEN the later one returns.
- **Determinism.** GIVEN no write between two calls THEN the outputs are byte-identical.
- **Empty.** GIVEN an empty store WHEN `profile` runs THEN an empty list, exit 0. GIVEN `-k 0` THEN an empty list.
- **Read-only.** GIVEN any store WHEN `profile` runs THEN no row changes and the WAL does not grow.
- **Smallest accepted input:** one row with a non-empty title. **Smallest rejected:** `profile -nope`, an unknown flag — rejected like every other verb.

## KPIs & failure signal
- **Good:** 8 of 10 fixed standing-fact probes are answered by the digest alone without a `search`; the read holds p95 < 50 ms at 10⁴ rows; the digest holds ≤ 4 KB at 500 factors.
- **Off-switch:** after 20 sessions that touched the store, the digest was read in fewer than half. That means nothing asked for current state, and the verb is removed rather than kept as decoration. Measured from agent transcripts — the store keeps no telemetry and must not start.
- **Removal is the recovery path:** deleting the verb restores the frozen surface exactly, because the store itself holds no state for this feature.

## Non-functional requirements
- **Size:** ≤ 64 factors and ≤ 4 KB of JSON, at any store size. Exceeding it is a bug, not a tuning knob.
- **Latency:** p95 < 50 ms for the digest read at 10⁴ rows — the bound `search` already holds.
- **Read-only:** the digest never writes; pinned by a test.
- **Portability:** one file, offline, no network listener, no external service.
- **Privacy:** none added. No telemetry is collected to measure the KPI.

## Assumptions & dependencies
- No embedder is configured, and the CLI never sets one. Factor identity is a key the caller writes, never a stem match or a similarity guess. *Invalidated by:* `SetEmbedder` being called — reopen identity, because a wrong merge loses a fact silently.
- Callers write one title per factor consistently. *Invalidated by:* splits appearing in practice — fixed by caller discipline, never by store inference.
- The store states no inference. *Invalidated by:* an explicit decision to add verdicts — re-lock `memory-store.md` first, then this doc.
- The verb surface may grow by exactly one read-only verb, and one more is not free. *Invalidated by:* any further verb — reopen this contract rather than drift.
- `memory` keeps one writer, and a read takes no write lock. *Invalidated by:* a second writer — reopen the concurrency contract.

## Data requirements
- **Required input:** rows in `memory` whose title is non-empty after trimming. Nothing else.
- **Output:** the digest as JSON, one entry per factor: identity, value, count, first, last, live/closed split, scopes, plus the `omitted` count.
- **Must survive:** nothing new. The digest is never persisted, so `memory` stays the only durable state, content stays immutable, and `schema_version` stays 6.
- **Edge — empty record:** an empty store, or `-k ≤ 0`, returns an empty list rather than an error. A title of only spaces is not a factor. A forgotten row counts in nothing.

## Rollback & recovery
- **N/A for data:** the feature adds one read and no state, so there is nothing to migrate, repair, or downgrade.
- **Reverted means:** delete the verb, the store method and the AGENTS.md line. `schema_version` does not move, so a half-applied change is impossible.
- **One wrecking failure:** not destruction — disclosure. The digest prints stored text, which can reach an agent transcript. Security owns it.

## Security & compliance
- The digest echoes stored text to stdout, and so possibly into an agent transcript. That is the trust boundary `get` and `search` already have: one local user, one local file, loopback only. No new boundary is opened.
- Three rules: the value only, never `extra` or the provenance columns; one line, cut at 200 characters; no redaction, because the store cannot distinguish a secret from a sentence and a guessed redaction would corrupt stored text on output.
- Compliance: N/A — one user, one local file, no network listener, no external service, no telemetry.

## Verification plan
- Acceptance cases 1–8 → fixture tests in `store_test.go`, one per case, in the style of the existing lifecycle tests.
- Empty store and space-only title → one test each.
- Read-only → a test that pins the absence of a write path, like `TestMatchersHasNoWritePath`, plus a WAL-size check across one call.
- Bound → a test that puts 500 factors and asserts ≤ 64 rows, ≤ 4 KB, and an exact `omitted`.
- Latency → a `cmd/bench` p50/p95 line for the digest at 10⁴ rows, beside the existing search numbers.
- KPI probes → ten standing-fact probes in `cmd/bench`, each scored yes or no, target 8 of 10.
- CLI → `cmd/memstore/main_test.go`: `-k 200` clamps to 64, `-k 0` is empty, `-scope a,b` parses, an unknown flag is rejected.
