# AGENTS.md — memstore

## Intent

A portable memory store: one SQLite file holding every memory, so knowledge crosses
the session and the repo boundary. A fuzzy query must return the right memory in the
top 5 when the exact words are absent. CLI only, single user, no server.

## How to run / build / test

`go` is not on PATH — the flake supplies Go 1.27 (`go.mod` requires it). Every
command below was run from the repo root and passed.

```bash
# Enter the toolchain (go, gopls)
nix develop

# Build · test · vet · format
nix develop --command go build -o /tmp/memstore ./cmd/memstore
nix develop --command go test ./...
nix develop --command go vet ./...
nix develop --command gofmt -l cmd *.go   # prints nothing — vendor/ excluded; only gofmt walks it

# Run — the store is $MEMSTORE_DB, or ~/.memstore/memory.db
/tmp/memstore put -kind decision "text"
/tmp/memstore search -k 5 "query"          # JSON, best first

# The NFR benchmark: p50/p95 at 1x and 10x, recall probes, size
nix develop --command go run ./cmd/bench
```

## Hot invariants

- **Recall is bm25 over a porter-stemmed index, OR-joined.** A query that shares
  any stemmed term with a memory — an inflected form, or one token of several —
  returns it, best first. With zero shared stems and no embedder, the store
  returns fewer results, never a wrong one. The vector matcher is absent unless
  `SetEmbedder` is called; do not call the lexical path "search by meaning".
- **The verb surface is frozen**: `put get search recent link close forget stats
  import`. The CLI carries no logic — it parses, calls the store, prints.
- **Content is immutable.** Only `status`, `deleted_at` and `tier` change after a
  put. A correction is a new row plus `close`, never an update.
- **`forget` is one-way.** It sets `deleted_at`, and no verb restores it — a
  mistaken forget needs raw SQL.
- **FTS is external-content**: the text is stored once, never copied into the index;
  the index is tokenized by the porter stemmer.
- **`schema_version` is 5.** The porter tokenizer is baked into `record_fts` at
  CREATE, so `Open` rebuilds the index from `memory` when it upgrades a v3 (or v2)
  file; a v1 file is refused rather than silently read as 1970. v4 → 5 adds the
  partial unique index on `(origin, origin_id)` and changes nothing else.
- **Provenance is content, and `(origin, origin_id)` is unique when it is set.**
  `Put` writes the four fields it is handed — an empty `origin` becomes `native`,
  and an empty `origin_id`, `ref` or `fs_node` becomes NULL — and refuses a
  duplicate pair with `ErrDuplicateOrigin`, so a caller decides whether a re-run
  is a skip. The index is partial: NULL `origin_id` never collides.
- **One writer at a time.** One local file, no server, no network.
- **A commit invalidates the locked specs.** `check-specs.sh` measures the whole
  project, so any commit past the sha a marker names refuses the `code` entry gate;
  a change to store behaviour must re-lock in the same dispatch.

## Architecture elevator

Store owns the `memory` table and its lifecycle, and refuses search and ranking.
Matchers own recall fusion and refuse writing. The CLI owns the verb surface and
refuses business logic. `Put` and the Importer both write the provenance columns,
from opposite directions: a caller's value, or fs's event.

```
cmd/memstore/     — the CLI: parse args, call the store, print JSON. No logic.
cmd/bench/        — the NFR harness: p50/p95 at 1x and 10x, recall probes, size.
store.go          — Store: schema, the lifecycle verbs, import, projections.
matchers.go       — Matchers: lexical ∪ relation-walk ∪ optional vector, read-only.
.agents/specs/    — the locked contract and its system design.
.agents/dispatch/ — the card each slice was built from.
```

## Deeper docs

- `.agents/specs/memory-store.md` — the locked spec: acceptance criteria, NFRs, KPIs.
- `.agents/specs/memory-store-design.md` — the locked system design: components, data
  model, failure modes, scale assumptions.

Last verified: 2026-10-17.
