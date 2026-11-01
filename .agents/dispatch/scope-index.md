# Card — the scope index: make a scope listing cost what a scope listing should

## Context

fs's slice 2 is delivered (cap:t-e360586e) and its switch is **off** — not because it is wrong,
but because it is slower than the thing it replaces. AC8 measures the switched read at **1.26× to
1.40×** the event store's cost against a **1.25×** ceiling, and spec D6 reverts a read that
regresses. This round is the fix for that arm, filed as **memstore:t-23a2c408**.

The cause is measured, not guessed. `Recent(scope, k)` (store.go:680) is memstore's only
scope-listing verb, there is **no index on `scope`**, and it also sorts what it returns:

```sql
SELECT <memoryCols> FROM memory WHERE deleted_at IS NULL [AND scope = ?]
                                     ORDER BY ts DESC, id DESC LIMIT ?
```

So every call scans the whole `memory` table and sorts it, while fs's event store walks a primary
key. The user's real file is the shape to reason about: **~4,600 `fs:` rows plus 341 personal
rows**, and fs's read asks for one scope's worth.

## Step 0 — measure before you change any DDL

This round's whole value is the measurement, so take it first and report it in the node:

1. On a **copy** of a real store (or a generated one at the real shape — say which), `EXPLAIN QUERY
   PLAN` for `Recent(scope, k)` with a scope and a k that fs actually uses. Paste it. `SCAN memory`
   plus a temp b-tree for the ORDER BY is the expectation; confirm it rather than assuming.
2. A baseline: N warm runs, medians **and** spread, so the after-number has something honest to be
   compared with.
3. Say which half dominates — the scan, the sort, or the per-row decode. **If the sort is not a
   significant part, stop and say so**: the fix is then a different thing and this card is wrong.

## What to build

- **`schema_version` 5 → 6**, and the index must reach **existing** files through the version check —
  the design's own rule is that an additive change without a bump lands in new files only. A file at
  5 must open, report 6, and carry the index.
- **The index must serve the predicate AND the ORDER BY.** `deleted_at IS NULL` and `scope = ?` are
  both equality constraints, and `ORDER BY ts DESC, id DESC` is the order — choose the column order so
  SQLite uses one index for all three. The evidence is an `EXPLAIN QUERY PLAN` at the real shape
  showing a **SEARCH** on the index and **no** "USE TEMP B-TREE FOR ORDER BY".
- **Fail loudly** if the index cannot be created: `Open` names it and refuses, the same rule the
  `(origin, origin_id)` index already follows. Never swallow it.
- **Do not add speculative indexes.** If the same index also serves `Search` or `Stats`, say so; do
  not add one for them without a measurement of its own.

## The alternative, to be measured rather than argued

**fs does not need ordering for this read** — it derives state itself and sorts its own answer. So a
verb that *lists* a scope without the `ORDER BY`, or streams it, may buy more than any index. If it is
cheap, measure both arms — the index, and the same query with no `ORDER BY` — and **build the one the
measurement names**. If the answer is a new verb rather than an index, that is a legitimate outcome of
this round; say so and stop rather than forcing the index.

## Acceptance — prove the READ's cost, not the index's existence

1. `nix develop --command go test ./... -count=1` green, `go vet ./...` clean, `gofmt -l` silent.
2. The benchmark, before and after, one stated protocol (warm, N runs, medians **and** spread), with
   the numbers pasted. A speedup with no spread is not evidence on a machine running other agents.
3. The `EXPLAIN QUERY PLAN`, before and after, pasted.
4. The upgrade path proven on a real v5 file: it reports 6, carries the index, keeps its rows, and a
   file that cannot take the index fails loudly naming it.
5. **One honest line** on what `Recent` still costs and what now dominates it — the row count, the
   decode, or something not yet measured. This is the line that tells fs whether the switch can be
   flipped, so a vague answer is worse than a disappointing number.

## What this round does NOT prove

Whether fs's AC8 now passes. That is a separate fs round: it re-runs its own harness
(`BenchmarkPendingProjectionCost` vs `BenchmarkPendingLogCost` on a copy of the live store) against
this commit and either flips `switchedReads = []string{"pending"}` or reports the new ratio.

## Out of scope

Any fs change, the one-line switch, the content-drift verb that the errata (skills:t-222f53e7) names
as missing — that is a separate decision about memstore's surface, not this round — and any other
memstore verb.

## Files

store.go
store_test.go
cmd/bench
AGENTS.md
.agents/specs/memory-store.md
.agents/specs/memory-store-design.md
