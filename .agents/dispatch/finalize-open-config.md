# Card — finalize: Open's configuration phase, and the comment that outlived its rule

Two open gaps, one file, one commit. Both are the last known warts on a store that is now
published and installed at `9e51a69`, and this round is what closes the memstore project out.

## The defect class

**`Open`'s configuration phase assumes it is alone and that the path is plain.** It handles
neither contention nor a path it did not choose. Two instances, one function:

### Instance 1 — the WAL switch races a second opener (`store.go:214-227`)

`PRAGMA journal_mode = WAL` returns `SQLITE_BUSY` on a contended file, and **SQLite does not
invoke the busy handler for a journal-mode change** — so `busy_timeout`, which fixed everything
else, cannot cover this one. Measured by the reviewer on this machine, after `5ec1768`:

```
two concurrent `memstore put` against a FRESH file:  17 of 20 trials failed
ten concurrent:                                     6 of 10
before the fix (`5ec1768`'s parent):                20/20 and 10/10
an instrumented copy names the failing step:        set-wal
```

First-run only, once per store file — but it is the *first run* of a published tool.

**Direction, and the choice is yours to make and record:** a journal-mode change cannot be
retried by the busy handler, so the bound must be chosen here rather than inherited — tolerate
`SQLITE_BUSY` at that step and re-read the mode (another opener may have set it), or serialize
the one-time switch. State which you chose and why.

### Instance 2 — the DSN is concatenated (`store.go:165`)

The DSN is built as `path + "?_txlock=immediate"`, so a `?` in the path truncates it at the
driver:

```
MEMSTORE_DB="/tmp/path?q/memory.db" memstore put   -> exits 0, writes /tmp/path
stats on the intended path                          -> no such file or directory
```

A command that exits 0 and writes a *different file than it names* is the worst shape here: the
caller is told it succeeded. Make the path unparseable as parameters — a proper DSN/URI with the
path escaped — or refuse loudly. Never write somewhere else and report success.

### Rider — the comment above `const schema` (`store.go:92-94`)

Same file, same subject, so it rides: it still states the rule the design doc no longer holds
(*"Adding it is additive … an existing v2 file gains the table on open"*). Since `5ec1768` an
additive table lands only when `schema_version` is bumped, and that policy is written in
`.agents/specs/memory-store-design.md` under Data model. The comment sits exactly where the
author who must bump the version reads, so it invites the silent no-op it describes. This is gap
`memstore:t-33f96521`; the v2 sentence itself is still true, the present-tense generalisation is
not.

## Acceptance — the checks this is held to

1. **The repro, repeated**: N ≥ 20 rounds of two concurrent `put` processes against a **fresh**
   file. **Every** process exits 0 and every round's row count is right — 0 failures, against
   17/20 before. Paste the loop and the counts, and same for ten concurrent.
2. **The DSN**: `MEMSTORE_DB="/tmp/path?q/memory.db" memstore put …` either writes *that* file
   (verify with `stats` on the intended path) or refuses with a clear message. It must not exit 0
   having written elsewhere, and a test must pin whichever behaviour you chose.
3. The comment above `const schema` states the version-bump policy the design doc holds.
4. `go test ./... -count=1` green (including `TestConcurrentWritersQueue`), `go vet` and
   `gofmt -l .` clean — the driver is on a clean, published tree, so nothing here is allowed to
   regress.
5. `"$FS_BIN" script check-specs.sh --dir .agents/specs` exits 0 after your commit, with both
   markers rotated to it.

## Out of scope

The store-layer replacement in fs (`skills:t-b1a2e486`, parked on purpose: slices 2–4 and the
`Put`-provenance round await the user). Nothing about the projection, nothing in another repo,
and no new verb — the verb surface is frozen.

## Budget

One round. `store.go`, `store_test.go`, and the two spec markers. If the WAL fix needs a
mechanism wider than `Open`, stop and report rather than widening.

## Resources

The main checkout of `~/Documents/apps/memstore`. No worktree — `check-specs.sh` measures the
whole project, so a commit ahead of the lock refuses the entry gate, and a worktree forked from
HEAD would not carry the untracked state the same gate reads. One sibling pane via `--deliver`;
`fs close` tears it down. Verify with `herdr workspace list` that no workspace was opened under
your name.

## Files

store.go
store_test.go
