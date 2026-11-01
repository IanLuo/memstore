# Card — the third instance of the class: the schema's table and its row arrive in two commits

Small by design: one window, one requirement, one test that must stop being flaky.

## The defect class

**`Open` assumes it is alone while another opener is halfway through creating the schema.**
The finalize round fixed the two instances the card named (the WAL switch, the DSN) and left a
third, which its own new test exposes about **2 times in 200**:

```
--- FAIL: TestConcurrentOpensOnFreshFile
    round 8, opener 0: memstore: read schema_version from …/memory.db: sql: no rows in result set
```

**Not** `database is locked` — that was the old failure. This one is a race between two
*statements*, and the mechanism is visible in the file:

```
const schema (store.go:140-142)   CREATE TABLE IF NOT EXISTS schema_version (version …);   <- commits
                                  INSERT INTO schema_version(version)
                                      SELECT 4 WHERE NOT EXISTS (SELECT 1 FROM …);          <- commits later
Open (store.go:199-204)           read A: SELECT count(*) FROM sqlite_master WHERE name='schema_version'
                                     -> 1, because the other opener's CREATE has committed
                                  read B: SELECT version FROM schema_version
                                     -> NO ROWS, because its INSERT has not
```

Two concurrent openers on a fresh file: the loser reads a table that exists and a row that does
not, fails closed, and takes down the CLI invocation. Under WAL the snapshot makes the window
real rather than theoretical. **Measured by the cap at HEAD `f61295a`: 2 failures in 200 runs**,
and by the reviewer at 2–6 in 100.

## What the fix must accomplish

1. **A concurrent opener must never fail on a fresh file, at any concurrency.** The schema must
   arrive *atomically* — a reader sees the table and its version row, or neither. The obvious
   root cause is that `schema`'s statements commit separately; wrap the application in one
   transaction, or make the read tolerate the half state — and say which you chose and why. Both
   are acceptable; a retry loop around read B is the weakest of the three, because it treats a
   half-created schema as normal.
2. **The test stops being flaky.** `TestConcurrentOpensOnFreshFile` must pass **200 consecutive
   runs**, not most of them. A test that fails 1% of the time is worse than no test: it will be
   re-run until it passes, and the bug it caught will be forgotten.
3. **The 5 s bound lives in one place.** `PRAGMA busy_timeout = 5000` (store.go:181) and
   `5 * time.Second` in `switchToWAL` are the same intent written twice, and the review flagged
   that they can drift. Derive one from the other, or name them as one constant.

## Acceptance — the checks this is held to

1. `go test -run TestConcurrentOpensOnFreshFile -count=200 .` → **0 failures** (2/200 at HEAD).
2. The CLI form, ten concurrent `put` on a fresh file, five rounds → every process exits 0 and
   the row count is 10 each time. Paste the loop.
3. Both earlier instances still hold: the WAL repro is 0 failures over 20 rounds at two
   concurrent, and `MEMSTORE_DB` with a `?` writes the file it names or refuses — never a
   different file with exit 0.
4. `go test ./... -count=1` green, `go vet` clean, `gofmt -l cmd *.go` prints nothing.
   **The card's earlier `gofmt -l .` was the cap's error and is withdrawn** — a bare `gofmt -l .`
   lists 9 pre-existing `vendor/` files, and AGENTS.md already states the first-party form.
5. `"$FS_BIN" script check-specs.sh --dir .agents/specs` exits 0 after your commit, with both
   markers rotated to it and left uncommitted, which is this repo's convention.

## Out of scope

The DSN and WAL decisions already made and reviewed; the store-layer replacement in fs
(`skills:t-b1a2e486`, parked); anything about the projection.

## Budget

One small round: `store.go` and `store_test.go`. If the atomicity fix needs a wider change to
`Open`, stop and report rather than widening — a smaller true fix beats a larger speculative one.

## Resources

The main checkout of `~/Documents/apps/memstore`, no worktree (the entry gate reads untracked
state, and `check-specs.sh` measures the whole project). One sibling pane via `--deliver`;
`fs close` tears it down. Verify with `herdr workspace list` that no workspace was opened under
your name.

## Files

store.go
store_test.go
