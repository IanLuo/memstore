# Task: the matcher boundary — Matchers owns ranking and fusion; Store refuses them

You are a worker. The cap dispatched this. Do not spawn agents.

**The class: a locked-design boundary the code never drew.** The design says — `Store`:
"Refuses: search, ranking, telemetry"; `Matchers`: "owns recall fusion (lexical ∪ relation-walk
∪ optional vector). Refuses: writing." The code keeps `lexicalRank`, `vectorRank` and
`rrfFuse` as `Store` methods. Every review since slice 1 has flagged this (slice-1 F4 lineage,
three times, never blocking). Extract it — then the code matches the locked doc and the review
loop stops. Design is correct as written; **do not re-lock it.**

Read before you plan: the locked `.agents/specs/memory-store-design.md` Components ·
`memory-store.md` AC3 · `store.go`'s `Search`/`lexicalRank`/`vectorRank`/`rrfFuse`/`SetEmbedder`
· the slice-4 review `memstore:t-812378fa` (its one accepted finding — the tokenless query —
is fixed here too).

## Files

```
store.go
matchers.go
store_test.go
matchers_test.go
```

---

## 1 · the `Matchers` surface

A `Matchers` type (same package — a subpackage would import-cycle on `Memory`) that **owns**
listening and ranking:

- `lexical(q, k)` — the FTS bm25 ranking (moved, unchanged).
- `vector(q, k)` — cosine over `record_vec`, only when an embedder is set and rows have
  vectors (moved, unchanged).
- `fuse(...)` — the RRF merge (moved, unchanged).
- A single `Search(q, k)` entry the store calls.

`Matchers` reads **through a narrow interface the store implements** (match by query, fetch
rows/vectors by id). It holds **no write path** — no `Exec`, no `Insert`, no schema — its only
handle is the read interface.

`Store` keeps persistence and the lifecycle verbs; its `Search` becomes one delegation to its
`Matchers`. `SetEmbedder` still lives on the store (it owns the vector enablement) but feeds
the matchers.

## 2 · the tokenless fix (the slice-4 review's one accepted finding)

Today a query with **no lexical tokens** (`"!!!"`, `"   "`, `"---"`) returns empty without ever
consulting the vector matcher (the slice-4 probe: `calls=0, hits=[]`). Fix: when an embedder
**is** configured, a tokenless query still runs the vector matcher (embedder called; results
possible); with no embedder it stays `[]` — the degrade rule is unchanged.

## 3 · tests

- Move/keep every existing ranking test green (extend, never delete).
- **Boundary assertions:** `bm25`/`cosine`/`rrf` logic lives in `Matchers`, not reached
  through `Store`; `Matchers` has no method that writes (grep the interface's methods).
- **Tokenless:** with the stub embedder, `Search("!!!", k)` calls the embedder (assert via a
  counting stub) and can return a hit; without an embedder it returns `[]`.
- The slice-4 probes (AC3, no-demotion, atom-on-write, degrade) still hold verbatim.

---

## Acceptance check

1. `nix develop -c go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` — green.
2. **Boundary:** `Store` has none of bm25/cosine/RRF logic (they live in `Matchers`); `Matchers`
   cannot write (no store write handle). Show the code and a test that pins it.
3. **Degrade gate:** no embedder → behaviour and results identical to slice 4 (existing lexical
   tests pass unchanged).
4. **Tokenless:** with the stub, `Search("!!!", k)` invokes the embedder and may return a hit;
   without an embedder it is still `[]`.
5. All slice-4 probes (AC3, exact-hit first, atom-on-write, import embed/no-embed) still pass.
6. No new dependency; `schema_version` stays 3 (the version check is untouched).
7. Say what you did **not** verify.

## Constraints

- Go via the flake devshell (`nix develop -c …`); no new dependency.
- **Do not edit `.agents/specs/`** — the cap rotates the markers. (The design is already
  correct for this change; nothing to amend.)
- **This shell is zsh: an unquoted `$var` does not word-split**, and `$?` after a pipe is the
  last command's status.
- **Write your own node with an UNQUALIFIED id**, re-read it with `fs log --node <id>`, mark it
  done. (`fs` is not on PATH — hand-find it as previous workers did.)
- **This card's dispatch budget is 3.**

## Reporting — required

Note your own work on your own node in `memstore` — the cap created it. Result first, then the
exact commands and what they printed, then one line naming what you did not verify. Then mark
it done.

Use the `dev-task` skill.