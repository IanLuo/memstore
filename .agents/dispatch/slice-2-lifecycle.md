# Task: slice 2 — the lifecycle verbs and relations

You are a worker. The cap dispatched this. Do not spawn agents.

**The class: the rest of the frozen verb surface.** Slice 1 shipped `put · get · search`.
Slice 2 ships the remaining verbs the locked design names — `close · forget · recent · link ·
stats` — plus the one accepted-but-deferred bound fix, and the relation surfacing that makes a
search result *situated* (spec AC2).

Read before you plan — **the locked docs govern**: `.agents/specs/memory-store.md` (spec) and
`.agents/specs/memory-store-design.md` (system-design). Also read `.agents/dispatch/slice-1-store.md`
and the store's current state in `store.go`. Do **not** edit the specs.

One design call is made for you: the design says "a full edge table is not required" — that
means the *wide, denormalised* edge design from an earlier draft. The `link` verb needs a
home for an arbitrary relation added after both memories exist, so a **thin `edge` table
(`from_id, rel, to_id, ts`) is the honest minimum** — not the wide one, just enough to store
an edge and walk it.

## Files

```
store.go
store_test.go
cmd/memstore/main.go
```

---

## 1 · the tsMin bound (accepted minor, now fixed)

`store.go`'s `tsMin` guard uses `1678-09-21`, one year later than the true `UnixNano`
lower bound (`1677-09-21T00:12:43.145224192Z`). Fix the constant; make the comment and the
test boundary exact.

## 2 · `Close(id, reason, replaced_by?)`

**Owns: retiring a record.** Sets `status = closed` (and not before). With `replaced_by`,
sets `supersedes` and records the edge (see 5 — your `close` is the user of `link`).
**Must not:** delete bytes, or touch `deleted_at`. Unknown id → error; a second `Close` is a
no-op or an error — decide and say which in your report (the "one way" rule: a closed row's
status is its own).

## 3 · `Forget(id)`

**Owns: soft deletion** — sets `deleted_at`; the row is hidden from `Get`, `Search`,
`Recent`, and `Stats`' open count, while its bytes remain (spec AC4). **Must not:** destroy
bytes, or restore anything — this slice has no un-forget; hiding is the point. Unknown or
already-forgotten id → error. Reversible only by deleting the row, which no verb does.

## 4 · `Recent(scope?, k)`

**Owns: finding by time** — newest `ts` first, `deleted_at` rows excluded, `scope` AND-ed,
`k <= 0` → empty. **Must not:** rank by meaning (that is `Search`'s job).

## 5 · `Link(from, rel, to)` + the relation walk

**Owns: one edge** — a thin `edge` table (`from_id, rel, to_id, ts`; PK covers
`(from_id, rel, to_id)`; both ends reference `memory`). A repeated identical `Link` is a
no-op; an unknown endpoint is an error. `rel` is open text (`superseded-by`, `found-by`,
anything). **Must not:** write memory rows.

**The walk:** a `Search` hit whose `supersedes` column or an outgoing `superseded-by` /
`found-by` edge names another memory **surfaces that id on the result** (spec AC2: searching
`D1` carries `D2`'s id when `D1` is superseded by `D2`). The columns already do this for
`supersedes`; extend it to the edges so a found memory shows where it sits.

## 6 · `Stats()`

**Owns: the store's own shape** — total rows, open rows, forgotten rows, file size in bytes
(and the WAL's, while open), and the newest and oldest `ts`. **Must not:** write anything.

## 7 · the CLI

Add `close`, `forget`, `recent`, `link`, `stats` to `cmd/memstore`, matching the existing
`put|get|search` surface and its error handling (one `memstore: ` prefix — that is now `A2`
of the previous round, do not regress it).

---

## Acceptance check

1. `nix develop -c go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` — green.
2. **Spec AC4:** `Forget(id)` → `Get`/`Search`/`Recent` no longer return it; the db file's
   size is unchanged by the forget (bytes remain); a second `Get` errors.
3. **Spec AC2:** `Close(D1, "reason", D2)` → `Search` finds D1 and the result carries D2's
   id; D1 reads `status=closed`, its text intact.
4. **tsMin:** a `Put` at exactly `1677-09-21T00:12:43.145224192Z` succeeds; one nanosecond
   below is refused — with the message naming the bound.
5. **Link + walk:** `Link(A,"superseded-by",B)` → a `Search` hit on A surfaces B's id;
   `Link` with an unknown endpoint refuses; a repeated `Link` is a no-op.
6. **Recent:** newest-first, `scope`-filtered, forgotten rows absent, `k=0` → empty.
7. **Stats:** the open count matches the number of live `Put`s; the bytes are the actual
   file size.
8. **Regression:** empty-text `Put` still refused; one `memstore: ` error prefix throughout.
9. Say what you did **not** verify.

## Constraints

- Go via the flake devshell (`nix develop -c …`); no new dependency.
- **Do not edit `.agents/specs/`** — the cap rotates the markers.
- Never write to `~/.fs/store.db` or `~/.fs/registry.db`.
- **This shell is zsh: an unquoted `$var` does not word-split**, and `$?` after a pipe is the
  last command's status.
- **Write your own node with an UNQUALIFIED id**, re-read it with `fs log --node <id>`, mark it
  done. (`fs` is not on PATH here — hand-find it as previous workers did.)
- **This card's dispatch budget is 4.**

## Reporting — required

Note your own work on your own node in `memstore` — the cap created it. Result first, then the
exact commands and what they printed, then one line naming what you did not verify. Then mark
it done.

Use the `dev-task` skill.