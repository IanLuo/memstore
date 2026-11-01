# Card — `Put` discards the provenance it is handed, and there is no idempotency key

Small, one verb, one schema bump. It blocks fs's slice 3: fs projects its event log into
memstore through `Put`, and the projection needs an `origin_id` a re-run can recognise.

## The defect class — a value accepted and silently dropped

`Memory` has four provenance fields. `Put` writes one of them as a constant and the other
three as NULL, whatever the caller supplied:

```
store.go:456   INSERT INTO memory (… origin, origin_id, ref, fs_node)
               VALUES (…, "native", nil, nil, nil)     <- caller's values discarded
```

That is the **whole fixture family**: `origin`, `origin_id`, `ref`, `fs_node`. Slice 1 of fs
worked around it by stuffing provenance into a JSON `Extra` envelope. That workaround is why
this round exists, and it must not survive as the reason to keep the columns empty.

Second half of the same class: **nothing makes a re-put idempotent.** The only uniqueness is
`memory.id` and the `edge` primary key. `Import` dedups by its own `SELECT EXISTS` on
`origin = 'imported' AND origin_id = ?` — which is a read-then-write race and is scoped to
imported rows only. A caller that folds the same event twice gets two rows.

## The cap's rulings — do not re-litigate these; report instead if one is wrong

1. **All four columns are written from the struct.** `origin` defaults to `'native'` when the
   caller leaves it empty, so an existing caller changes nothing.
2. **Empty provenance fields insert NULL, never `''`.** This is the trap: a non-NULL empty
   string becomes an index key, and the second row with `('native','')` collides. `OriginId`,
   `Ref`, `FsNode` each map `""` → `NULL`.
3. **Idempotency is keyed on `(origin, origin_id)`, not `origin_id` alone** — the same scope
   `Import` already uses. A partial unique index:
   `CREATE UNIQUE INDEX … ON memory(origin, origin_id) WHERE origin_id IS NOT NULL`.
   A global `origin_id` index is **rejected**: fs's migration will project the same fs event
   ids that `Import` also carries as `'imported'`, and the two shapes must be able to coexist.
4. **A duplicate is refused, not skipped.** `Put` returns an exported sentinel,
   `ErrDuplicateOrigin`, that `errors.Is` matches, so the caller decides whether to be
   idempotent. Silent success on a duplicate would hide double-writes, and memstore is strict
   everywhere else (`Get` errors on missing, `Put` refuses empty text).
5. **`schema_version` 4 → 5**, and the version check carries the change into existing files
   (the design's own rule: an additive change without a bump lands in new files only).
6. **`Import`'s own INSERT path is not touched.** It does not call `Put`. But the new index is
   created over its rows too, so if any pre-existing file holds duplicate `('imported', id)`
   pairs, `Open` must **fail loudly** — never swallow the error and never skip the index.
7. `Put`'s transaction still embeds in the same transaction when an embedder is configured;
   an embed failure still leaves no row.

## What to build

- `store.go`: the INSERT takes the four values; `GET` already returns them (`memoryCols`
  covers them), so no read change. The sentinel and its detection at the INSERT error.
- The schema: version 5, the partial unique index as its own `IF NOT EXISTS` statement, applied
  inside the same transaction as the rest of the version bump.
- `AGENTS.md`: two invariants are now false — "`schema_version` is 4" and "The Importer is the
  only writer of the provenance columns". Fix both, and say what replaces them.
- The specs, re-locked in this dispatch: `memory-store.md` (the `Put` verb's contract) and
  `memory-store-design.md` (the provenance paragraph at :17, the data model, the version rule).
  Leave the markers dirty — this project's convention, because a committed marker always names
  an ancestor.

## Tests — the fixture family and the barrier, in the first round

Every one of these is acceptance, not suggestion:

1. **Round-trip, all four fields.** `Put` a memory carrying `Origin/OriginId/Ref/FsNode`;
   `Get` returns them byte-identical. This is the assertion that fails today.
2. **The default is unchanged.** A `Put` with no provenance is `origin='native'` and three
   NULLs in the file — read the raw row, not just the struct.
3. **Empty is NULL.** Two consecutive `Put`s that leave `OriginId` empty both succeed. Under
   the bug this test guards (`''` as an index key) the second one fails.
4. **The same `(origin, origin_id)` twice** → second refused, `errors.Is(err,
   ErrDuplicateOrigin)`, and the file still holds one row.
5. **The same `origin_id` under a different `origin`** → both succeed. The coexistence ruling.
6. **A barrier test.** N goroutines that `Put` the *same* `(origin, origin_id)` from a shared
   start line: exactly one row, exactly one success, N−1 `ErrDuplicateOrigin`. Staggered
   goroutines are not a concurrency test — the same lesson the four `Open` rounds were taught.
7. **Upgrade in place.** Build a v4 file with the pre-change schema, then `Open` it: the file
   reports 5, the index exists, and existing rows keep their values.
8. **The loud failure.** A file seeded with two identical `('imported', x)` rows must make
   `Open` return an error naming the index — assert the error, not a crash.
9. `Import` remains idempotent and still leaves native rows alone (`store_test.go` already has
   `TestImportIdempotentAndLeavesNativeAlone`; it must stay green untouched).

## Acceptance

```bash
nix develop --command go test ./... -count=1    # the FULL suite, not only the new tests
nix develop --command go vet ./...
nix develop --command gofmt -l cmd *.go         # prints nothing
```

Plus: the card's files are the only ones changed, staged by name, and one commit. The cap
pushes and reinstalls the profile afterwards — **do not push**.

## Out of scope

Any change to fs, `Import`'s INSERT, the verb surface, the matchers, the CLI, the embedding
path. Do not extend the schema beyond the one index.

## Files

store.go
store_test.go
AGENTS.md
README.md
.agents/specs/memory-store.md
.agents/specs/memory-store-design.md

(`README.md` added by the cap after round one reported it: `README.md:70` still says
`schema_version` is 4. The omission is the cap's — the worker found it and correctly refused to
edit a file outside the list.)

## Budget

One round. If the index cannot be created over existing data for a reason other than genuine
duplicates, stop and paste the error rather than working around it.
