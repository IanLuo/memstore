# memstore

A portable memory store. One SQLite file holds every memory, so knowledge crosses
the session and the repo boundary.

The store is single user, local and offline. It has no server and no network.

## Verbs

```
put -kind KIND [-scope S] [-title T] [TEXT...]  append a memory; TEXT from args or stdin
get ID                                          one memory, as JSON
search [-k N] QUERY...                          matching memories, as JSON, best first
close ID [REPLACED_BY]                          close a memory; optionally retired in favour of another
forget ID                                       hide a memory (soft delete; its bytes remain)
recent [-k N] [SCOPE...]                        newest memories first, as JSON
link FROM REL TO                                record an edge; the walk surfaces superseded-by / found-by
stats                                           the store's shape: counts, file bytes, ts bounds
import SRC_STORE.db                             ingest fs events one-way (provenance recorded, idempotent)
```

The store is `$MEMSTORE_DB`, or `~/.memstore/memory.db` when that is unset.

```bash
memstore put -kind decision "search returns best first"
memstore search -k 5 "how are results ordered"
```

## Test

`go` is not on `PATH`; the flake supplies Go 1.27. From the repo root:

```bash
nix develop --command go test ./...
nix develop --command go build ./...
nix develop --command go vet ./...
nix develop --command gofmt -l .          # vendor/ is third-party; check first-party files only
```

The NFR benchmark is `nix develop --command go run ./cmd/bench`.

## Install

The flake publishes `packages.default`, built from the vendored tree.

```bash
nix profile install .#default
# or, without installing:
nix build .#default && ./result/bin/memstore
```

`devShells.default` stays available: `nix develop` enters a shell with `go` and `gopls`.

## Limits

These are not caveats to read past; they bound what the store can do.

- **Recall is lexical.** `search` is bm25 over a porter-stemmed index, OR-joined
  across the query's terms. `gate` reaches `gates`, and a query with one matching
  token of five still ranks the row. **No embedder exists anywhere in this
  project**, so a query that shares zero stems with a memory returns nothing.
  There is no search by meaning.
- **`forget` is one-way.** It sets `deleted_at`; no verb restores it. A mistaken
  `forget` needs raw SQL.
- **`schema_version` is 4.** `Open` rebuilds the FTS index when it upgrades an
  older file; a v1 file is refused rather than read as 1970.
- **Content is immutable.** Only `status`, `deleted_at` and `tier` change after a
  `put`. A correction is a new row plus `close`, never an update.
- **One writer at a time.** One local file, no server, no network.
