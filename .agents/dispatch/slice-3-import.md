# Task: slice 3 — `import`, the last frozen verb: one-way ingest of fs's events

You are a worker. The cap dispatched this. Do not spawn agents.

**The class: the verb that makes memstore a replacement.** The frozen surface is
`put · get · search · recent · link · close · forget · stats · import`; slices 1 and 2 shipped
the first eight. `import` ingests the events of the other store (fs's `~/.fs/store.db`) into
this one, **one-way** — nothing existing is touched, original rows are never modified (design:
"Importer — owns one-way ingest. Refuses: modifying rows that already exist"; spec: "one-way
import of fs's events").

**One design fact, decided for you:** the locked `memory` table (15 columns, `schema_version`
2) has no provenance, so an `import` would be un-verifiable and un-idempotent. The design's
own rule — *"a new column is additive, never a rewrite"* — authorises the extension: add
`origin TEXT NOT NULL DEFAULT 'native'`, `origin_id TEXT`, `ref TEXT` (commit sha), `fs_node
TEXT` (the fs node key), an index on `fs_node`, and bump `schema_version` to **3** (reuse the
A1 pattern: Open validates the version, refuses anything unexpected). Native rows keep
`origin='native'`; imported rows record every field.

Read before you plan: the locked `.agents/specs/memory-store.md` + `memory-store-design.md` ·
the fs store's `events` table shape (id, timestamp, project_id, node_id, parent_node_id, type,
payload, commit_sha — you may inspect `~/.fs/store.db` **read-only**) · the existing
`schema_version` validation in `store.go` (A1 of the last round).

## Files

```
store.go
store_test.go
cmd/memstore/main.go
```

---

## 1 · provenance columns + `schema_version` 3

Add the four columns, the `fs_node` index, the version bump, and the version-3 validation.
Prove the **v2→v3 upgrade on open** (a v2 database opens; its rows are still read; new rows
write the new columns); an unknown version still refuses. Do not invent a migration of the
data — additive means existing rows simply have default `origin`, NULL elsewhere.

## 2 · `Import(srcStorePath, opts) (imported, skipped int, err error)`

Reads an fs-style store (`events` table) and appends one memory row per event:

- `scope` ← event `project_id`
- `kind` ← mapped from `type` — `decision-recorded→decision`, `knowledge-added→knowledge`,
  `task-created→task`, `report-recorded→report`, `plan-recorded→plan`, `delivery-recorded→event`,
  `status-changed→event`, anything else keeps its fs `type` name (kinds are open)
- `title` ← the payload's `goal`/`summary`/`line`/`body` (first present, trimmed), else the type
- `text` ← the payload's text-ish field, else the raw JSON (trimmed)
- `extra` ← the remainder of the payload, if any
- `ref` ← `commit_sha` · `origin='imported'` · `origin_id` ← the event `id` · `fs_node` ← `node_id`
- `parent` ← resolved to the imported memory's id via `fs_node` (a `parent_node_id` that names no
  imported row is left NULL — it may arrive in a later event, and import is resumable)
- `ts` ← the event timestamp · `status` stays `open`
- **Idempotent:** a row whose `(origin, origin_id)` exists is skipped, counted, never
  re-created or updated. **Nothing existing is modified.**
- An unreadable or non-events source fails with an error and writes nothing.

## 3 · the CLI

`memstore import <src-store.db>` — prints `imported N, skipped M`. One `memstore: ` error
prefix (the A2 rule — do not regress it).

---

## Acceptance check

1. `nix develop -c go build ./...`, `go test ./...`, `go vet ./...`, `gofmt -l .` — green.
2. **Upgrade:** a v2 db opens (existing rows readable), new rows carry the new columns;
   `schema_version` reads 3; an unknown version still refuses. Show it.
3. **Mapping:** a fixture fs-store (you build it in the test — sample events across ≥2
   projects and ≥3 nodes, one parent→child pair) imports so every mapped field lands:
   `scope`, `kind`, `title`, `text`, `ref`, `origin='imported'`, `origin_id`, `fs_node`,
   and the `parent` resolves to the right local id. Assert each.
4. **Idempotency:** re-importing the same fixture imports 0, skips N; no duplicates in the
   table; an existing **native** row is byte-unchanged by the import.
5. **Errors:** a garbage source fails with an error and writes nothing; a parent key with no
   matching imported row leaves `parent` NULL and does not fail the import.
6. **Live read probe:** against the real `~/.fs/store.db` **read-only**, report how many rows
   an import would write and the split by `kind` — do not write to it.
7. **Regression:** all 25 existing tests still pass (extend, never delete).
8. Say what you did **not** verify.

## Constraints

- Go via the flake devshell (`nix develop -c …`); no new dependency.
- **Do not edit `.agents/specs/`** — the cap rotates the markers.
- Never write to `~/.fs/store.db` or `~/.fs/registry.db` — reading them read-only is allowed.
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