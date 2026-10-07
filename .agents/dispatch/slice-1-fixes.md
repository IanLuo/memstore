# Task: fix round — slice 1 review findings F1, F2, F3 (+F5)

You are a worker. The cap dispatched this. Do not spawn agents.

**The class: a lifecycle the store does not honour, and a file nobody restricted.** The
review of `5aa287a` (node `memstore:t-5b013ab0`) found two real defects of the same shape —
a column the design treats as state is written but never read — plus a time round-trip that
does not round-trip.

Read before you plan:
- the reviewer's node `memstore:t-5b013ab0` — it carries the repros and the exact commands;
- `.agents/specs/memory-store.md` (AC4) and `.agents/specs/memory-store-design.md`
  (`memory` columns; Security) — **do not edit them**;
- card `.agents/dispatch/slice-1-store.md` — the slice this repairs.

## Files

```
store.go
store_test.go
cmd/memstore/main.go
```

---

## F1 — `Get`/`Search` return soft-deleted rows  (blocking, code)

`Put` accepts a caller `DeletedAt` and writes it; neither `Get` nor `Search` filters
`deleted_at`. Repro (from the review): `Put` with `DeletedAt=now` → `Get` returns it with
`err == nil`, `Search` finds it. Spec **AC4** says a forgotten row is hidden from reads, so
the column is state the store must honour.

**Fix:**
- `Get` and `Search` exclude rows with `deleted_at` set (`deleted_at IS NULL`).
- **Do NOT filter `status`** — a closed/superseded row stays findable; spec AC2 requires
  `Search` to return a superseded row carrying its successor's id. Only deletion hides.
- Slice 1 has no `forget` verb, so make the unreachable obvious: **refuse a caller-supplied
  `DeletedAt` in `Put`** (deletion is `forget`'s job, a later slice) — one way in, one way out.

## F2 — the store file is not user-only  (blocking, code)

Design Security: "Store local, user-only permissions." `Open` mkdirs the parent `0700` but
never restricts the **file**; sqlite creates it under the umask (`-rw-r--r--` under 022).

**Fix:** `chmod 0600` the database file after open (and assert the parent dir is `0700`).

## F3 — `Ts` does not round-trip  (code)

`Ts` is stored as unix **seconds**, so a caller time with sub-second precision or a non-UTC
zone comes back different (`Equal == false`). Card acceptance 2 says `Put`→`Get` is
byte-identical; the suite only asserts `Ts.IsZero()`, so the claim is untested for time.

**Fix:** persist the instant losslessly (e.g. unix **nanoseconds**) so `Put`→`Get` compares
`Equal`. If you conclude second-resolution is the right storage, do not leave the acceptance
claim standing — say so and state the narrower claim for the cap to record.

## F5 — CLI double error prefix  (cosmetic, code)

`main.go` prints `memstore: ` plus an error already prefixed `memstore: `, giving
`memstore: memstore: put: text is empty`. Print one prefix.

**Not in scope (accepted by the cap, do not change):** F4 (Store owns bm25 — the boundary
lands with the matcher slice) · F6 (`Id` naming — enforced nowhere, renaming breaks callers).

---

## Acceptance check

1. `nix develop -c go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` — green.
   **Rebuild/commit order per the card:** commit, then the cap checks.
2. **F1:** a `Put` that sets `DeletedAt` is **refused**; a row with `deleted_at` set is
   **absent** from `Get` and from `Search`; a **superseded/closed** row is still returned by
   `Search` and carries its successor (AC2 unchanged). Show each.
3. **F2:** open a store under `umask 022`; the db file is `0600` and its parent `0700`.
   Show `stat`.
4. **F3:** `Put` a memory whose `Ts` has sub-second precision → `Get` returns it with
   `Ts.Equal(put) == true`. Show the test.
5. **F5:** the CLI's empty-text error prints exactly one `memstore: ` prefix.
6. All 9 existing tests still pass (extend, don't delete) — say which you added.
7. Say what you did **not** verify.

## Constraints

- Go via the flake devshell (`nix develop -c …`); no new dependency.
- **Do not edit `.agents/specs/`** — the cap rotates the markers.
- Commit by name; leave `.agents/specs/` and `.agents/dispatch/` to the cap.
- **This shell is zsh: an unquoted `$var` does not word-split**, and `$?` after a pipe is the
  last command's status.
- **Write your own node with an UNQUALIFIED id**, re-read it with `fs log --node <id>`, mark it
  done. (`fs` is not on PATH here — hand-find it as the previous workers did.)
- **This card's dispatch budget is 3.**

## Reporting — required

Note your own work on your own node in `memstore` — the cap created it. Result first, then the
exact commands and what they printed, then one line naming what you did not verify. Then mark
it done.

Use the `dev-task` skill.
