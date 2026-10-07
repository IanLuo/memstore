# Card — recall must answer a paraphrase, not only an exact-token query

## The defect class

**The lexical matcher requires every query token, verbatim, over an unstemmed index.**
So a query that does not contain all of a memory's words in exactly their stored form
returns nothing — which is the failure the spec was written to remove ("recall is
exact-token, so a query whose words are absent returns nothing", memory-store.md,
Problem & who breaks). Measured by the cap on 2026-10-07 against a store holding
"memstore replaces the fs store layer: one SQLite file, FTS external-content, no
mirror table" and "twilight routes plain work nodes with no gates; its reports land on
the cap pane":

| query | result | why |
|---|---|---|
| `one file` | the row | both tokens present |
| `one file hold` | **nothing** | `hold` is absent, and every token must match |
| `gate` | **nothing** | the index holds `gates`; no stemming |
| `twili`, `SQLit`, `persist` | **nothing** | no prefix matching |
| `does one file hold the memory` | **nothing** | any function word kills it |

## Instances under the class

1. **The tokenizer.** `record_fts` is created with the default tokenizer, so `gate`
   cannot reach `gates`. `store.go`, the `const schema` block, `CREATE VIRTUAL TABLE
   record_fts`.
2. **The query construction.** The lexical match is built as AND over the query's
   tokens, so one absent word returns zero rows instead of ranking the rest. This is
   in `matchers.go` (the lexical matcher) — read it before you plan; the cap has not
   pinned the call site, only the behaviour.
3. **The benchmark states its own query set as AND.** `cmd/bench/main.go` prints
   `"2-token AND, drawn from the 256-token synthetic vocabulary"` and draws two tokens
   from the same vocabulary, so it measures a query shape the matcher no longer has.
   This project's convention is that a benchmark states the set it measured; leaving
   that line is a benchmark describing a different one.

## The contract this must satisfy — decided by the user, 2026-10-07

Do not treat the two halves as one:

- **With no embedder configured**, a query that shares **any** lexical handle with a
  memory — a stemmed form, or one token out of five — returns that memory in the
  **top 5**. Ranked by bm25, best first. `one file hold` returns the row above;
  `gate` reaches `gates`; a 6-token query with one matching token ranks it in.
- **With zero lexical overlap**, nothing can be found lexically, and the store must
  return **fewer results, never a wrong one** (the design's fail-open-on-read rule).
  `how is memory persisted locally` against the row above has no shared stem and no
  embedder anywhere in this project, so returning `[]` stays correct — that gap is
  the vector matcher's, and it is **optional and unchanged by this card**.

So AC3 is re-anchored, not weakened: today it passes only against a test-stub
embedder; after this it passes **with no embedder at all**, for every case with a
lexical handle, and it states plainly where the lexical boundary is.

## Also on this card, and why it is not a separate dispatch

- **The stale comment at `store.go:92-94`** ("Adding it is additive — schema_version
  still names the timestamp format … so an existing v2 file gains the table on open")
  states the rule this card's version bump replaces. It sits directly above `const
  schema`, which is the block you are editing, so it is one file, one commit and one
  lock rotation instead of two dispatches. This is gap `memstore:t-33f96521`.
- **Commit the project's untracked record in the same commit.** The version bump
  forces a lock rotation anyway, and that is the cheapest correct moment:
  `AGENTS.md` (the agent context, written 2026-10-07 and never committed),
  `.agents/dispatch/` (every card, including this one), and the `.agents/specs/`
  documents as the spec participant leaves them. Nothing else will commit them, and
  one `git clean -fd` takes the lot today. **Stage by name — never `git add -A`, and
  never `git checkout --`, `restore` or `stash` anything under `.agents/specs/`.**

## The version bump

The tokenizer is baked into the FTS table at creation, so this is a schema change and
under the policy this project now states (`memory-store-design.md`, Data model: *"a
schema change reaches an existing file only through a `schema_version` bump"*) it takes
`schema_version` **3 → 4** with `record_fts` rebuilt from `memory` — the design already
says the index is "rebuildable from `memory`". A v3 file must be upgraded on open by
the same path that already upgrades v2; a file already at 4 must take no write lock on
open (that property is `5ec1768`'s and must not regress).

## Acceptance — the check this is held to

A test with **no embedder set** — assert that, do not assume it — where:

1. `one file hold` against the row above returns it in the top 5;
2. `gate` returns the row whose text says `gates`;
3. a query that is a memory's own text with one word removed returns it first;
4. a query with **no** shared stem returns no rows (the fail-open half, so the fix
   cannot pass by ranking noise);
5. a v3 file opened by the new build gains the porter index and keeps its rows, and
   `record_fts` still stores no copy of the text (external-content is unchanged);
6. `schema_version` reads 4 after the upgrade;
7. the full suite, `go vet ./...` and `gofmt -l .` are clean, and Open still takes no
   write lock on a file already current (keep `TestConcurrentWritersQueue` green).

Paste the commands and their output on your node. The benchmark's stated query set
must be corrected in the same commit, and say on the node what it now states.

## Out of scope

The vector path, `SetEmbedder`, `record_vec`, and RRF fusion weights beyond what the
lexical change forces. No new dependency. No query language, no ranking by `importance`
or `tier`, no synonym list, no configurable tokenizer.

## Budget

**One round.** Files are those declared below. If the change needs a file outside that
list, or a second round, stop and report rather than widening.

## Resources

The delivery splits one sibling pane in the cap's tab.

- **Pane** — created by `--deliver`; `fs close` closes it.
- **Worktree — none, deliberately.** This dispatch works in the main checkout at
  `/Users/ianluo/Documents/apps/memstore`, for the same two reasons as the last one:
  `check-specs.sh` measures the whole project, so any commit ahead of the lock refuses
  the `code` entry gate; and a worktree forked from HEAD would not carry the untracked
  `AGENTS.md` that the same gate reads. Do not create a worktree or a branch.
- **Workspace** — none. Verify with `herdr workspace list` that nothing was opened
  under your name before you report done.

## Files

store.go
matchers.go
store_test.go
matchers_test.go
cmd/bench/main.go
AGENTS.md

`.agents/dispatch/` and the two `.agents/specs/*.md` documents are committed by this
change but are deliberately **not** listed above: the lock convention leaves the specs
modified on purpose, and `check-deliverable` requires every declared path to be both
committed and clean. `AGENTS.md` is listed because it is in the diff and must end up
committed.
