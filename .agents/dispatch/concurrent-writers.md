# Card — concurrent callers must queue, not fail

## The defect class

**A write lock taken on every call, by a caller that waits for nobody.** An
operation that needs the write lock must either not take it when nothing needs
changing, or wait for it. `Open` does neither. A second process racing the first
is refused instead of queued, and — because the lock is taken before any read —
the refusal lands on read-only verbs too.

## Instances under the class

1. **No busy-wait.** `Open` never sets `PRAGMA busy_timeout`, so SQLite returns
   `SQLITE_BUSY` on the first collision rather than waiting for the lock.
2. **A write transaction on every open.** `Open` runs `PRAGMA journal_mode = WAL`,
   the whole `schema` DDL, one `ALTER TABLE` probe per provenance column, and
   `UPDATE schema_version SET version = ? WHERE version = 2` unconditionally. So
   `get`, `search` and `stats` — which write nothing — take the write lock.
3. **A comment asserting the opposite.** `store.go:164`: *"a single connection
   keeps concurrent callers from racing two connections into SQLITE_BUSY."*
   `SetMaxOpenConns(1)` serializes connections **within one process**. The CLI is a
   new process per command, which is exactly the case that fails, so the comment
   states the guarantee that does not hold.

## Measured by the cap on HEAD `d735e3f`

Delete the store between rounds. Do not re-run these in the cap pane — they are here
as the before-picture and as the after-check.

- Two concurrent `memstore put`, three rounds: `SQLITE_BUSY (5)` on apply schema,
  `SQLITE_BUSY_SNAPSHOT (261)` on read, and a third failure — **3 of 6 writes
  stored**.
- Ten concurrent `memstore put`: **3 of 10 stored**, all with
  `memstore: apply schema to <path>: database is locked (5)`.

## Why it matters

One user, many sessions. `memory-store.md` states the audience as *"one person
across sessions and projects, plus the agents acting for them"*, so two panes
writing at once is the intended shape, not an edge case. Today one of them keeps
nothing. The exit code is 1, but a caller that does not read it loses the memory.

## Acceptance — the check this is held to

A test in `store_test.go` that opens **two** `Store` handles on one file and writes
from both concurrently. Two handles are two `sql.DB` pools, so they contend the way
two processes do; a goroutine test on one handle cannot reproduce this defect, which
is why it survived this long. Every write must succeed and the stored row count must
equal the writes attempted plus the seed row. This fails before the fix.

Then run the CLI form and paste the counts into the node: two concurrent `put` × 3
rounds and ten concurrent `put`, each on a fresh file — every process exits 0, and
the count is **7** and 11 respectively (1 seed + 3 rounds × 2 = 7; 1 seed + 10 = 11).
Both counts were measured; the 4 this line first carried was arithmetic done wrong in
the cap's head, and the worker and the reviewer each reported 7 against it.

## Out of scope

Writer *semantics*. SQLite still allows one writer at a time; this makes the second
caller wait instead of fail. Do not widen it into multi-writer support, retry loops
in the CLI, or a queue.

## Budget

**One round.** Beyond `store.go` and `store_test.go` you are also authorised to touch
`.agents/specs/memory-store.md` and `.agents/specs/memory-store-design.md` — for the
lock marker, and for the clause your change made stale. The `spec` participant is in
your plan and the marker has to name your commit, so that revision is yours. Nothing
else: a second round, or any other file, means stop and report.

The `## Files` list below stays `store.go` and `store_test.go` **on purpose**. Every
path it declares must be both committed and clean at close, and the lock convention
leaves the docs modified on purpose — so declaring them would make your own close
refuse. Here the list is deliberately narrower than the diff. Say so on your node.

If no acceptance criterion moved, say exactly that rather than inventing a revision; a
marker that rotates with no clause behind it is still a claim that the doc was re-read.

## Do not discard the cap's revision

`.agents/specs/memory-store.md` and `.agents/specs/memory-store-design.md` are already
modified when you start. Those bytes are the cap's uncommitted re-locks, not yours:
read them, leave them, and never `git checkout --`, `git restore`, `git stash` or
commit them as your own. Take `git diff -- .agents/specs/` before your first edit, so
your node can say which hunks were already there and which are yours.

## Resources

The delivery splits one sibling pane in the cap's tab; the agent runs there.

- **Pane** — created by `--deliver`. `fs close` closes it.
- **Tab** — the cap's own; not yours to close.
- **Worktree — none.** This dispatch deliberately has no worktree: it works in the
  main checkout at `/Users/ianluo/Documents/apps/memstore`. Two reasons, both hard.
  `check-specs.sh` measures the whole project, so any commit ahead of the lock refuses
  the `code` entry gate — and a worktree forked from HEAD would not carry the untracked
  `AGENTS.md` that the same gate reads. Do not create a worktree or a branch; if one
  appears, say so on the node rather than tidying it.
- **Workspace** — none created. Verify with `herdr workspace list` that nothing was
  opened under your name before you report done.

State on the node, in the closing round, that pane, branch and workspace were each
checked, and name what you found.

## Files

store.go
store_test.go
