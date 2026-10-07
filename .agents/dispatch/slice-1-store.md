# Task: slice 1 — the store, and the first three verbs

You are a worker. The cap dispatched this. Do not spawn agents.

**The locked design governs:** `.agents/specs/memory-store-design.md` (system-design) and
`.agents/specs/memory-store.md` (spec). Read both before you plan. **Do not edit them** —
they are locked.

## Files

```
flake.nix
go.mod
store.go
store_test.go
cmd/memstore/main.go
```

## Handover from a dead worker

A previous worker died mid-slice on a provider repetition loop; **nothing is committed** (HEAD
is still `603c98f`). On disk: `flake.nix`, `flake.lock` (the devshell works — `nix develop -c go
version` → go1.27.0), `go.mod`, `go.sum` (dep `modernc.org/sqlite`), `store_test.go` (8 red
tests). **Absent:** `store.go`, `cmd/memstore/main.go`. Its plan and assumptions are on node
`memstore:t-925378d6` — read it before you plan. Keep what is right, rewrite what is not; you
are not bound by it.

---

## 1 — the schema

`memory` — the only source of truth. Columns, from the locked design:

```
id · ts · kind · scope · title · text · extra · importance · tier · status
   · deleted_at · parent · supersedes · found_by · hash
```

- **Content is immutable; lifecycle is not.** `id`…`text`, `extra`, `hash` are written
  once and never updated. `status`, `deleted_at`, `tier` may change. This narrow exception
  is what removes the need for event rows — do not add a second table of "changes".
- **`record_fts` — FTS5 with external content** over (`title`, `text`). The text must be
  indexed **in place**, not copied: no `record_fts_content` table. A second copy of every
  payload is the exact defect the design exists to avoid.
- A `schema_version` row.
- **Open `kind`:** an unknown kind is stored, not refused. **Empty `text` is refused.**

## 2 — the first three verbs and a CLI

- `Put(m) → id` — append one memory; refuse empty `text`.
- `Get(id) → m` — one memory by id.
- `Search(q, k) → []` — **lexical only** for this slice (FTS5). The relation-walk and the
  vector matcher are later slices; do not build them now.
- A minimal CLI: `memstore put|get|search`, opening `$MEMSTORE_DB` or `~/.memstore/memory.db`.

---

## Acceptance check

1. `nix develop -c go build ./...` and `nix develop -c go test ./...` are green. The host has
   **no `go` on PATH** — add a `flake.nix` devshell pinning `go_1_27` (mirror the pattern in
   `../skills/flake.nix`).
2. **Round-trip:** `Put` then `Get` returns the memory byte-identical (spec acceptance 1).
3. **Empty text refused; an unknown `kind` accepted** (spec acceptance 6).
4. **No text copy:** after inserting a long unique string, `record_fts_content` does **not**
   exist (proves external content). Assert it.
5. **Search finds it:** a query whose words occur in a memory returns that memory; ranking is
   lexical for now. A query with no match returns empty, not an error.
6. **Immutability:** no `UPDATE` touches a content column; assert with a test that a second
   `Put` of the same `id` is refused (or writes nothing) rather than overwriting.
7. Say what you did **not** verify.

## Constraints

- Go, pinned via a flake devshell (`go_1_27`) — this machine has no `go` on PATH.
- SQLite driver: prefer a **pure-Go** one (`modernc.org/sqlite`) so the binary stays
  portable with no cgo. **If FTS5 is not available there, stop and report it** rather than
  falling back silently — FTS5 external content is the design's size guarantee.
- No other new dependency without saying why an existing one will not do.
- **Do not edit `.agents/specs/`** — locked; the cap rotates markers.
- Never write to `~/.fs/store.db` or `~/.fs/registry.db`.
- **This shell is zsh: an unquoted `$var` does not word-split**, and `$?` after a pipe is the
  last command's status.
- **Write your own node with an UNQUALIFIED id**, re-read it with `fs log --node <id>`, and
  mark it done.
- Do **not** close any node or gap; the cap does after reading your node.
- **This card's dispatch budget is 3.**

## Reporting — required

Note your own work on your own node in `memstore` — the cap created it. Result first, then
the exact commands and what they printed, then one line naming what you did not verify. Then
mark it done.

Use the `dev-task` skill.
