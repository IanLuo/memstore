<!-- specs:locked:f61295a 2026-10-16 type=system-design -->

## Link contract
- **upstream** (this doc relies on): .agents/specs/memory-store.md
- **referrers** (must cite this when they change): none

# System design — the memory store: one table, projections, fused recall
## Components & ownership
- **Store** — owns the `memory` table and its lifecycle. **Refuses:** search, ranking, telemetry.
- **Projections** — owns FTS (+ optional vectors). **Refuses:** being the source of truth.
- **Matchers** — owns recall fusion (lexical ∪ relation-walk ∪ optional vector). **Refuses:** writing.
- **CLI** — owns the verb surface. **Refuses:** business logic.
- **Importer** — owns one-way ingest. **Refuses:** modifying rows that already exist.

## Data model
- `memory`: `id · ts · kind · scope · title · text · extra · importance · tier · status · deleted_at · parent · supersedes · found_by · hash · origin · origin_id · ref · fs_node`.
- Provenance (added for `import`, slice 3 — additive columns, carried in at `schema_version` 3): `origin` (native|imported) · `origin_id` (the source's event id, import-unique) · `ref` (commit sha) · `fs_node` (the source's node key). A v2 file upgrades additively on open; a v1 (`5aa287a`) file is refused.
- **A schema change reaches an existing file only through a `schema_version` bump.** `Open` applies the schema, the provenance `ALTER` probes and the `fs_node` index only when the file's version is not current — that is what leaves an already-current file taking no write lock on open, which is what lets a reader race a writer. The `schema` statements — the tables, the trigger and the `schema_version` row — are applied in one transaction, so the table and its row become visible together: a second opener sees the whole schema or none of it, never the version table without the row that makes it readable. An additive table or column added without a bump therefore lands in a new file and nowhere else: bump `schema_version` when the shape changes, because the version check is what carries the change in. (Slice 2's `edge` table landed under the earlier rule, where `schema` ran on every open; that rule is gone.) The current version is 4: v3 → 4 is the porter index's rebuild, carried in by the same check.
- Immutable: content (`id`…`text`, `extra`). Mutable: `status`, `deleted_at`, `tier` — the one narrow exception to append-only, and what removes the need for event rows.
- `record_fts`: FTS5 external-content over (`title`,`text`) — no copy of the text. The `porter` tokenizer is baked into the table at CREATE, so raising the format from v3 to v4 drops and rebuilds the index from `memory` on open (the index is re-derived from the content table, never recopied).
- `record_vec`: optional (`id`, `vector`).
- No event rows: a record's state is a column, not another row.
- Edge input: empty `text` rejected; unknown `kind` stored (open label).

## Boundaries & interfaces
- Verbs (frozen): `put · get · search · recent · link · close · forget(soft) · stats · import`.
- One ability per verb, each with an explicit "must not"; the CLI carries no logic.
- The `memory` columns above are the frozen boundary; a new column is additive, never a rewrite.

## Failure modes
- Store corrupt or unreadable: fail-closed on write, fail-open on read (fewer results); `stats` reports.
- **Two openers race a fresh file:** the second sees the schema and its version row together or not at all, so it applies nothing or applies the idempotent schema again; the one-time WAL switch queues behind the same bounded busy timeout.
- `record_fts` out of sync: rebuildable from `memory`; search degrades to a scan.
- No vector model configured: the vector matcher is absent; lexical ∪ graph still answer.
- **Wrecking failure:** silent loss of a claim → append-only content + soft delete + content hash.

## Scale assumptions
- 1 user, 1 writer; 10²–10⁴ memories now, growth ~10²/day.
- Open is O(1) — no replay; search is O(index).
- Shard: one file per scope, opened lazily; cold rows tiered out of the hot index.
