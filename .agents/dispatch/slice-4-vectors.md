# Task: slice 4 — the open matcher surface: optional vector recall (spec AC3)

You are a worker. The cap dispatched this. Do not spawn agents.

**The class: the blurry-read half.** The design promises three matchers — lexical, graph,
vector — behind one open surface, where vectors are *optional*: "present only when a model
is configured; the store degrades to lexical+graph, never breaks." Slice 4 ships that surface
and the fusion, proves spec AC3 with a **deterministic stub embedder**, and leaves the real
model out: nothing in this environment can embed without an API key or a local model, so the
real provider is a later configuration (the user's call, recorded). The degrade path is a
first-class acceptance gate, not an afterthought.

Read before you plan: the locked `.agents/specs/memory-store.md` (AC3) +
`memory-store-design.md` (Components: Projections owns FTS (+ optional vectors); Matchers owns
recall fusion; `record_vec` optional; a configured model absent means lexical ∪ graph still
answer) ·
`store.go`'s current `Search` (the FTS bm25 walk) and the `schema_version` validation.

## Files

```
store.go
store_test.go
```

---

## 1 · `record_vec` — optional, never a version bump

A `record_vec(id INTEGER, vector BLOB)` table, **created only when an embedder is configured**
(and its `id` row alongside `memory`). It does **not** change `schema_version` — it is an
enablement, not a format; a build without an embedder never creates it and a db that has one
is opened fine and simply ignores it. Rows missing a vector are skipped by the vector matcher,
never fatal.

## 2 · the seam — `SetEmbedder(embed func(text string) ([]float32, error))`

A post-`Open` setter; the first call pins the dimension and (re)creates the table if needed.
When set: `Put` and `Import` embed the new row's text in the same transaction (an embedding
failure rolls the row back — no half-written memory). Rows written before the embedder was set
are left unembedded (their vector is absent; the matcher skips them). Nothing is re-embedded
in this slice.

## 3 · `search` fusion — lexical ∪ vector (graph keeps surfacing `Related`)

`Search` becomes a fusion: each registered matcher ranks, and the results are fused by
**Reciprocal Rank Fusion** (RRF — no score calibration; pick another fusion only if you can say
why in one line). The lexical matcher is today's FTS+bm25; the vector matcher is cosine over
`record_vec`, contributing **only when the table exists and has entries**. The relation walk
(rooted in `supersedes`, `found_by` and the `edge` table) keeps surfacing `Related` and does
not change.

**The degrade gate, as a test:** with no embedder, `Search` is byte-for-byte the current
behaviour — the lexically-identical cases rank identically, `record_vec` does not exist. With
the embedder, only the vector head changes the ranking.

## 4 · AC3, proven with the stub

A deterministic stub embedder (e.g., a fixed-dim bag-of-words hashing — no external model),
then: memory A and memory B share a concept word that the query does **not** contain; with
vectors on, `Search(query, 5)` puts B in the top 5 (spec AC3); with vectors off, B is absent
(no lexical overlap). Also assert an exact-match query still ranks the exact hit first — the
vector head must not demote a lexical bullseye.

## 5 · `stats`

Add vector coverage (`vector rows / total rows`) when configured; absent when not.

---

## Acceptance check

1. `nix develop -c go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` — green.
2. **Degrade gate:** no embedder → `record_vec` absent, `Search` identical to slice-3
   behaviour (the existing lexical tests pass unchanged); a db that already carries vectors
   opens fine without an embedder and ignores them.
3. **AC3:** with the stub, a query with zero exact-token overlap returns the meaning-matching
   memory in the top 5; with vectors off it does not. Show both.
4. **No demotion:** an exact lexical match stays first with vectors on.
5. **Atom-on-write:** a `Put` whose embedding fails rolls back (no row, no half-embedding);
   rows without vectors never break `search`.
6. **Import:** imported rows embed when an embedder is set; unmapped/dangling rows still
   import (their vectors absent).
7. **`schema_version` unchanged (3)** with vectors on; Open's unknown-version refusal intact.
8. All existing tests still pass (extend, never delete) — say which you added.
9. Say what you did **not** verify (e.g., a real model's embedding quality, cosine-vs-RRF at
   scale, a re-embed backfill).

## Constraints

- Go via the flake devshell (`nix develop -c …`); **no new dependency** — the stub is
  hand-rolled, no ML/vector libs.
- **Do not edit `.agents/specs/`** — the cap rotates the markers.
- Never write to `~/.fs/store.db` or `~/.fs/registry.db`.
- **This shell is zsh: an unquoted `$var` does not word-split**, and `$?` after a pipe is the
  last command's status.
- **Write your own node with an UNQUALIFIED id**, re-read it with `fs log --node <id>`, mark it
  done. (`fs` is not on PATH here — hand-find it as previous workers did.)
- **This card's dispatch budget is 3.**

## Reporting — required

Note your own work on your own node in `memstore` — the cap created it. Result first, then the
exact commands and what they printed, then one line naming what you did not verify. Then mark
it done.

Use the `dev-task` skill.