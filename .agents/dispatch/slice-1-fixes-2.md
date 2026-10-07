# Task: fix round 2 — the first fix round's own defects (A1, A2, A3) + one text overclaim (D1)

You are a worker. The cap dispatched this. Do not spawn agents.

**The class: a fix that changed a persisted unit without versioning it, an over-correction,
and a claim written rounder than the code.** The independent review of the first fix round
(node `memstore:t-7ab9a8be`, at `70e524d`) verified F1/F2/F3/F5 fixed and found five more.
Three are against that round and are fixed here; one behaviour is accepted (D1) and only its
**text** is corrected here.

Read before you plan: `memstore:t-7ab9a8be` (the findings and their repros) · the locked
`.agents/specs/memory-store.md` (AC1 was just narrowed) and `memory-store-design.md` ·
cards `.agents/dispatch/slice-1-store.md` and `slice-1-fixes.md`.

## Files

```
store.go
store_test.go
cmd/memstore/main.go
```

---

## A1 — the `ts` unit changed with no version bump  (blocking-ish, code)

`store.go:160` now writes `ts.UnixNano()`, but `schema_version` is still written as `1` and
is **never read**. A database written by `5aa287a` (`ts` in **seconds**, version `1`) is
therefore read as nanoseconds and silently shows 1970 instead of being refused.

**Fix:**
- New databases record **version 2** (the nanos format).
- `Open` **reads `schema_version` and refuses** any value it does not expect, naming the
  version it found — refuse, never migrate silently, and never misinterpret.
- A test: a db stamped version 1 (or any unknown) is refused on `Open`; a fresh db is
  version 2 and opens.

## A2 — CLI-local errors lost the program prefix  (code, cosmetic)

`cmd/memstore/main.go:37` now prints every error bare, so a CLI-local error reads
`get: bad id "x": …` with no `memstore:`. The card's F5 wording ("print the error bare")
aimed at the *double* prefix, not at stripping the program name.

**Fix:** add the `memstore: ` prefix **only when the message does not already carry it**, so
one prefix appears whether the error came from the store or the CLI.

## A3 — `UnixNano` wraps silently  (code, edge)

`ts.UnixNano()` silently wraps outside ≈1678–2262 (`Ts` = 2300 → 1715, no error). This
project's rule is "no silent wrong value".

**Fix:** `Put` refuses a `Ts` outside the range `UnixNano` can represent, with an error
naming the limit. In-range timestamps are unaffected.

## D1 — the parent-directory text overclaims  (text only — behaviour ACCEPTED)

`Open` restricts the file and its sidecars but **not a pre-existing parent directory**
(`store.go:90` runs `MkdirAll` only on the create path), so a caller-supplied directory
keeps its mode. **The cap accepts that behaviour** — the content files are `0600`, the
default path's directory is created `0700`, and chmod'ing a caller's existing directory
would be intrusive. **Do not change the behaviour.**

**Fix the words only**, which currently overclaim:
- `store.go`'s comment ("The parent directory is made 0700 above") must say the parent is
  created `0700` **when `Open` creates it**, and that a pre-existing directory is the
  caller's.
- `store_test.go`'s docstring must claim only what the test proves (the directory `Open`
  creates is `0700`; the file and sidecars are `0600`) — not "whatever the umask".

---

## Acceptance check

1. `nix develop -c go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` — green.
2. **A1:** `Open` on a db stamped `schema_version = 1` (or any unexpected value) **refuses**,
   naming it; a fresh db records `2` and opens. Show both, with the file's version read back.
3. **A2:** an empty-text CLI error prints exactly one `memstore: ` prefix; a bad-id CLI error
   ("CLI-local") also prints exactly one. Show both outputs.
4. **A3:** `Put` with a `Ts` outside the representable range is **refused** with a named
   limit; an ordinary `Ts` still round-trips. Show the test.
5. **D1:** the two comments no longer claim an unconditional `0700` parent; no behaviour
   changed (a pre-existing directory is still left as it is — say so).
6. All existing tests still pass (extend, never delete) — say which you added.
7. Say what you did **not** verify.

## Constraints

- Go via the flake devshell (`nix develop -c …`); no new dependency.
- **Do not edit `.agents/specs/`** — the cap rotates the markers.
- Commit by name; leave `.agents/specs/` and `.agents/dispatch/` to the cap.
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
