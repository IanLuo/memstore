# Card — the fourth instance, and the test that was too weak to see it

Small: one path, one requirement, and a concurrency test that starts its openers *together*.

## The defect class

`Open` assumes it is alone — instance four. The schema *application* is now one transaction
(`54c1ad8`), but the **v2/v3 upgrade that follows it is not**, and one statement in it is not
idempotent:

```
store.go:207   read the version                      BEFORE the transaction
store.go:244   tx = Begin(); tx.Exec(schema); Commit()      <- the schema is atomic now
store.go:276+  ALTER probes · CREATE INDEX · UPDATE version  <- OUTSIDE the transaction
store.go:302   CREATE TRIGGER memory_ai              <- BARE: no IF NOT EXISTS, and this one
                                                        is the v2/v3 REBUILD's own trigger
                                                        (the schema's copy at :143 IS idempotent)
```

So a second opener that read `v=3` before the winner committed enters the upgrade, **waits for
the write lock** (`BEGIN IMMEDIATE` via the DSN, so the busy timeout makes it wait properly),
and then runs its own `CREATE TRIGGER` — against a trigger the winner has already committed:

```
    memstore: upgrade …: trigger memory_ai already exists
```

**Measured by the reviewer: 2 of 5 concurrent openers on a v2/v3 file.** The cap's own attempt
found 0 of 25 — and the reason is instructive rather than exculpatory: **its openers were
staggered**, so only one ever entered the upgrade path. A concurrency test whose openers start
with jitter is not a concurrency test. That is why the new test below must use a barrier.

## What the fix must accomplish

1. **The whole upgrade is one transaction, with the version re-read inside it.** A second opener
   that lost the race must re-read `schema_version` under the lock, find the current version, and
   do *nothing* — no DDL, no rebuild, no UPDATE. This is the fix that removes the class rather
   than the symptom.
2. **The rebuild's DDL is idempotent anyway** — `DROP TRIGGER IF EXISTS memory_ai` before creating
   it, or `IF NOT EXISTS`, matching what `const schema` already does at :143. Belt and braces:
   the double-check is the fix, this is what keeps the next author safe.
3. **The test starts its openers together.** N goroutines or processes that all wait on one
   barrier and then open at once, on a **genuine v2/v3 file** (the FTS without the porter
   tokenizer, `schema_version` stamped 3 — the existing upgrade test's fixture recipe). It must
   fail before your fix and pass after, and it must not depend on scheduling luck.

## The contract stays as written — unless it cannot

The PRD now states: *"Concurrency: several processes may open the store, including the first
creation of the file; a caller waits for the lock rather than failing, and no write is lost."*
**Make that true for every path**, including the upgrade, and leave the text alone. If some path
genuinely cannot hold the guarantee, **narrow the text to what is true and name the excluded
path** — do not leave a claim the code cannot meet in the contract, and say which clause moved.

## Acceptance — the checks this is held to

1. The barrier test on a v2/v3 file: **0 failures**, repeated at least 50 times, and it fails at
   `0fdbed4` before your change. Paste both.
2. `go test -run TestConcurrentOpensOnFreshFile -count=200 .` → still 0 failures (it is green
   now; your change must not regress it).
3. Ten concurrent opens on a fresh file, five rounds → every process exits 0, rows correct.
4. `go test ./... -count=1`, `go vet`, `gofmt -l cmd *.go` clean.
5. `"$FS_BIN" script check-specs.sh --dir .agents/specs` exits 0 after your commit, markers
   rotated to it and left uncommitted (this repo's convention).

## Out of scope

The DSN and WAL decisions (made, reviewed, sound); the fresh-file fix (landed, 200/200); the
store-layer replacement in fs (`skills:t-b1a2e486`, parked).

## Budget

One small round: `store.go`, `store_test.go`, and the PRD only if the text must narrow.

## Resources

The main checkout of `~/Documents/apps/memstore`, no worktree. One sibling pane via `--deliver`;
`fs close` tears it down. Verify with `herdr workspace list` that no workspace was opened under
your name.

## Files

store.go
store_test.go
