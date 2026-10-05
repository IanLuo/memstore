# System design — the memory store: one table, projections, fused recall

## Components & ownership
- **Store** — owns the `memory` table and its lifecycle. **Refuses:** search, ranking, telemetry.
- **Projections** — owns FTS (+ optional vectors). **Refuses:** being the source of truth.
- **Matchers** — owns recall fusion (lexical ∪ relation-walk ∪ optional vector). **Refuses:** writing.
- **CLI** — owns the verb surface. **Refuses:** business logic.
- **Importer** — owns one-way ingest. **Refuses:** modifying rows that already exist.

## Data model
- `memory`: `id · ts · kind · scope · title · text · extra · importance · tier · status · deleted_at · parent · supersedes · found_by · hash`.
- Immutable: content (`id`…`text`, `extra`). Mutable: `status`, `deleted_at`, `tier` — the one narrow exception to append-only, and what removes the need for event rows.
- `record_fts`: FTS5 external-content over (`title`,`text`) — no copy of the text.
- `record_vec`: optional (`id`, `vector`).
- No event rows: a record's state is a column, not another row.
- Edge input: empty `text` rejected; unknown `kind` stored (open label).

## Boundaries & interfaces
- Verbs (frozen): `put · get · search · recent · link · close · forget(soft) · stats · import`.
- One ability per verb, each with an explicit "must not"; the CLI carries no logic.
- The `memory` columns above are the frozen boundary; a new column is additive, never a rewrite.

## Failure modes
- Store corrupt or unreadable: fail-closed on write, fail-open on read (fewer results); `stats` reports.
- `record_fts` out of sync: rebuildable from `memory`; search degrades to a scan.
- No vector model configured: the vector matcher is absent; lexical ∪ graph still answer.
- **Wrecking failure:** silent loss of a claim → append-only content + soft delete + content hash.

## Scale assumptions
- 1 user, 1 writer; 10²–10⁴ memories now, growth ~10²/day.
- Open is O(1) — no replay; search is O(index).
- Shard: one file per scope, opened lazily; cold rows tiered out of the hot index.
