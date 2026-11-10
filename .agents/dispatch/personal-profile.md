# Card — the personal profile: a bounded, read-only digest of current factors

## Context

The contract is `.agents/specs/personal-profile.md`, locked at `41bc56e`. It relies on
`.agents/specs/memory-store.md` for the store's guarantees and changes no schema. This card
is the build. Every acceptance criterion in the spec maps to a command or a test below, and
the spec is the source of truth where the two disagree. Report the disagreement; do not
quietly follow the card.

The spec is the record of decisions. Do not re-decide any of them here:

- Identity is `(kind, title)`, title trimmed and case-folded. Scope is an angle, not identity.
- Value is the newest **open** row's text. An all-closed factor has a count and no value.
- Order is last-seen desc, then count desc, then scope count desc, then title asc. Silent truncation is a bug.
  Build note: the card said "scope". A merged factor spans several scopes, so no single scope name can
  order it. The key is the distinct-scope count. The title tie-break is ascending.
- Bound is 64 factors and 24 KB of JSON. The byte budget is enforced while building output.
- The verb is `profile [-scope a,b] [-k 8]`. Cap 64, `-k ≤ 0` is empty.

## Step 0 — measure before you build

The spec makes two performance claims: a p95 under 50 ms at 10⁴ rows, and a read that is one
grouped pass. Measure both on a generated store at 10⁴ rows before you write the read:

1. Generate a store with 10⁴ rows over ~50 titles, with a mix of open and closed rows, using
   `cmd/bench`'s corpus helper. Say which.
2. `EXPLAIN QUERY PLAN` for the grouped read you intend to write. Paste it.
3. Warm N runs, medians **and** spread, for the same query. Paste the numbers.

If the plan shows a full scan with a temp b-tree and the p95 is over 50 ms, stop and report it
before you add an index. Do not add a new index without this measurement (spec NFR, Performance).

## What to build

- **`store.go`:** one method `Profile(scope []string, k int) (Digest, error)`. It is a read: it
  issues only `SELECT` against `memory`, and it writes nothing. The grouped query, the identity,
  the value rule and the order live here, not in the CLI.
- **`cmd/memstore/main.go`:** the verb `profile`. It parses `-scope` as a comma list and `-k` with
  default 8, calls the store, and prints JSON. It holds no business logic.
- **`AGENTS.md`:** the frozen verb line changes. It currently reads
  `put get search recent link close forget stats`; it must add `profile` and say it is read-only.
  AGENTS.md is a declared referrer of the spec, so this edit is part of the build, not a cleanup.
- **`cmd/memstore` usage text:** one line for `profile`.

Keep the JSON field names the spec implies and write them once: `factors`, `omitted`, and per
factor `kind`, `title`, `value`, `count`, `live`, `closed`, `first`, `last`, `scopes`.

## Acceptance — each spec criterion has one check

| Spec criterion | Check |
|---|---|
| Resolution | `store_test.go`: three live rows, one `(kind, title)`, one row out, count 3, first, last, newest open value |
| Retired version | `store_test.go`: newer closed + older open, value is the older open |
| All closed | `store_test.go`: factor with all rows closed, count, no value |
| Not a factor | `store_test.go`: empty title, space-only title, forgotten row, none counted |
| Title rule | `store_test.go`: `Build timeout` ≡ `  build TIMEOUT  `; `build timeout` ≢ `build-timeout` |
| Scopes | `store_test.go`: one title in `a` and `b`; `-scope a` gives 1; `-scope a,b` gives 2 |
| Bound | `store_test.go`: 500 factors, non-ASCII, 200-char titles and values; ≤ 64 rows, ≤ 24 KB, exact `omitted` |
| Order | `store_test.go`: `-k 1` returns the later-seen factor |
| Determinism | `store_test.go`: two reads, no write between, byte-identical output |
| Empty | `store_test.go`: empty store is an empty list; `-k 0` is empty |
| Read-only | a test that pins no `Exec(` or write SQL in the profile code, like `TestMatchersHasNoWritePath`, plus a WAL-size check across one call |
| CLI | `cmd/memstore/main_test.go`: `-k 200` clamps to 64, `-k 0` empty, `-scope a,b` parses, unknown flag rejected |

Performance, measured by `cmd/bench`:

- p50 and p95 of the digest read at 10⁴ rows, beside the existing search numbers. Target p95 < 50 ms.
- Ten standing-fact probes, each scored yes or no. Target 8 of 10 answered by the digest alone.

Run before you stop: `nix develop --command go test ./... -count=1`, `go vet ./...`, and
`gofmt -l cmd *.go web/*.go` (silent).

## What this round does NOT prove

- Whether a session reads the digest in practice. That is the off-switch, measured from
  transcripts after 20 sessions. The build cannot prove it.
- Whether the 8-of-10 probe target reflects real use. Probes are fixed by the bench, not by usage.

## Out of scope

- Read-time verdicts, trend windows, prose summaries, LLM calls, a persisted table, a new column,
  a schema bump, a UI route, and any new index without its own measurement.
- Any change to `Recent`, `Stats`, `Search` or `Get`.
- The redaction of stored text. The spec says no redaction, and this card does not add one.

## Files

store.go
store_test.go
cmd/memstore/main.go
cmd/memstore/main_test.go
cmd/bench
AGENTS.md
.agents/specs/personal-profile.md
