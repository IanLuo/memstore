# Task: probe — memstore's adoption evidence (NFR benchmark + fs-import dry-run)

You are a worker. The cap dispatched this. Do not spawn agents.

**Why this exists.** The cap ran both of these **by hand** in its own pane while answering "can we
use memstore for fs?" — which the standing rules forbid ("never do heavy work yourself"), and
the results have no node, no review and no reproducibility: the cap's own run carried a
measurement error it found only by luck. This card replaces that prototype with work that is
recorded. **The deliverable is evidence, not a change to the store** — if either part turns into
a code change to the store, stop and report it.

Read before you plan: the locked `.agents/specs/memory-store.md` (acceptance 3 + the NFRs) and
`memory-store-design.md` (scale assumptions) · the CLI `cmd/memstore` (its `import`, `search`,
`stats` verbs) · the fs store's `events` table shape.

## Files

```
bench_test.go        (or cmd/bench/main.go — a re-runnable harness, committed)
```

---

## Part A — the NFR benchmark

A **re-runnable** benchmark committed to the repo. At **N = 10³ and 10⁴** memories, measure:

- `Open` — fresh, and with rows present
- `Put` — throughput
- `Search` — p50 and p95 over a **fixed** query set (state the set: how many queries, drawn from
  what)
- **size ratio** — db bytes ÷ text bytes
- **10× delta** — search p95 at 10⁴ ÷ p95 at 10³

Report each against the locked spec's NFRs — `open < 50 ms`, `search p95 < 50 ms at 10⁴`,
`10× within 1.5×`, `bytes ≤ 2× text` — and say plainly which hold and which do not. Do not
smooth a miss; a failed bound is the finding.

> **Corrected 2026-10-07, text only, by the cap.** The last two figures in that list were the
> benchmark harness's own constants, never the locked spec's bounds: the spec bounded the 10×
> delta at 2× and the size at ≤4×, and since `cf6cb8a` it bounds the absolute only (`search`
> p95 < 50 ms at 10⁴), stating the ratio as a measured observation. The original sentence is
> left above unedited because it is the provenance of the defect: a harness constant read back
> to a worker as "the locked spec's NFRs" is how the bench came to print those numbers as
> "locked bounds". Do not report against the struck pair.

## Part B — the fs-import dry-run

Import a **copy** of the real `~/.fs/store.db` (never the original — the importer snapshots the
source; copy it anyway and say so) into a throwaway memstore, then report:

- rows imported / skipped; the kind split; db size; the ts bounds
- **recall probes** — 3–5 real queries (e.g. `lock regime currency`, `worker died resume`) with
  their top hits, and the same terms through `fs query` for comparison. (`fs query` returns
  `.data.events`, **not** `.data.results` — the cap lost a measurement to that.)
- **the shape observation:** the importer is event-per-row (~4100 rows over ~270 nodes). Is that
  a *memory* or a *log*? Say what a node-shaped import would change, and whether it matters for
  the fs-adoption question.

---

## Acceptance check

1. `nix develop -c go build ./...` and `go test ./...` green, **and the benchmark actually runs**
   (`-bench . -benchtime=…` or `go run ./cmd/bench`), not merely compiles.
2. The harness is committed and a stranger can re-run it from the command your node names.
3. **Every number on your node names the exact command that produced it and its raw output.**
4. The NFR table: bound, measured, pass/fail — including any failure.
5. The dry-run: mapping numbers + the recall probes + the events-vs-nodes observation.
6. Say what you did **not** verify.

## Constraints

- Go via the flake devshell (`nix develop -c …`); no new dependency.
- **Never write to `~/.fs/store.db` or `~/.fs/registry.db`** — copy first; the temp memstore is
  disposable.
- **This shell is zsh: an unquoted `$var` does not word-split**, and `$?` after a pipe is the
  last command's status.
- **Write your own node with an UNQUALIFIED id**, re-read it with `fs log --node <id>`, mark it
  done. (`fs` is not on PATH — hand-find it, as previous workers did.)
- **This card's dispatch budget is 3.**

## Reporting — required

Note your own work on your own node in `memstore` — the cap created it. Result first, then the
exact commands and their raw output, then one line naming what you did not verify. Then mark it
done.

Use the `dev-task` skill.