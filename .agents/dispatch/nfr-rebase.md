# Card — a locked NFR that a change invalidated is still quoted as current

## The defect class

**A bound the artifact no longer meets, still stated in the locked contract and still
asserted by the instrument.** Three revisions of this project have now re-locked the
performance bounds "to measured", each time from the behaviour that then existed — so
the bound is a *photograph* of one query shape, and the moment the shape changes the
photograph is false while every gate stays green, because nothing compares the two.

## Instances under the class

1. **The 10× growth bound, false at HEAD.** `.agents/specs/memory-store.md` states
   "10× the memories leaves recall p95 within 2× (measured 1.8×)" in **three** places —
   line 14 (Non-negotiable outcome), line 32 (an acceptance bullet) and line 40 (NFRs).
   Measured: parent `5ec1768` = **1.786×** (so "1.8×" IS the pre-change number), HEAD
   `b3e5e0c` = **3.778×** (search p95 **1.664 ms at 10⁴** over **0.440 ms at 10³**).
2. **The clause beside it, also false.** Line 32 ends "…and the absolute p95 stays
   sub-millisecond". It is 1.66 ms. The other bound in the same sentence — "the growth
   is fill — more matches per query as the corpus fills — not algorithmic blow-up" — is
   now the *correct* diagnosis and is the anchor for the repair.
3. **The instrument asserts bounds the contract does not hold.** `cmd/bench/main.go`
   keeps `nfrFlat = 1.5` and `nfrRatio = 2.0` under a comment calling them "The locked
   NFR bounds", and prints them as `locked bounds`, so it reports `FAIL` against
   numbers the committed spec does not carry. Measured sizes (4.361 at 10³, 3.568 at
   10⁴) do match the spec's cited figures; only the constants are stale.
4. **The size bound contradicts its own evidence.** Lines 36 and 42 promise "≈1.2× the
   text on a bulk payload and ≤4× on many small rows" while citing "measured 4.36× @10³"
   in the same breath. The promise is broken by the number printed next to it. The
   porter index changed the index shape, so all size figures want re-measuring.

## The decision — the user's, 2026-10-07

**Keep the OR design and rebase the NFR on the absolute.** Option (ii) — restricting
the query shape (AND first, OR fallback) to restore the 2× ratio — was **rejected**:
it restores the number by changing what the benchmark measures, leaving the paraphrase
queries that motivated the change unmeasured.

## What the spec must say

- **Bound the absolute, not the ratio.** `search` p95 < 50 ms at 10⁴ memories is
  already the KPI and already passes with roughly 30× headroom. That becomes the
  performance criterion.
- **State the ratio as a measured observation with its cause, never as a bound.** The
  cause is in the reviewer's words: OR over the query's stems saturates hits per query
  at k=10, so bm25 scores a candidate set that grows with N, where the AND query
  returned almost nothing. Cite **both** revisions — 5ec1768 (AND) 1.786× / 0.393 ms
  and b3e5e0c (OR) 3.778× / 1.664 ms — and the evidence node, so a reader sees the
  trade that was made rather than a bare number.
- **Delete the now-false sub-millisecond clause.** Do not soften it; it is false.
- **Repair the size claim rather than rounding it up.** State what the design holds per
  corpus size with the measured values attached, and if the honest figure is worse than
  the current wording promises, say that plainly on the node — a bound quietly raised
  to meet a measurement is the failure this card exists to stop.
- **All three sites move together.** Lines 14, 32 and 40 state the same claim; a
  revision that fixes one and leaves two is the defect repeating.
- **Say which claim moved, and why,** in the lock decision — the `spec` participant's
  own exit ask.

## The bench

`nfrFlat` and `nfrRatio` must be the numbers the spec actually states, and the output
must not call them "locked bounds" unless they are. A threshold tighter than the
contract, if you want one, is labelled an **internal target** and says so. The two
instruments must not be able to disagree again.

## Also on this card, because it is the same reading

`store.go:199` refuses an unexpected `schema_version` with
`"(unix-nanosecond timestamps)"`. At v4 the discriminator is the **porter tokenizer**,
so the parenthetical no longer says what distinguishes the format. Correct it. No test
asserts that string (checked), so this is the message and nothing else.

## Acceptance — the check this is held to

1. No **live claim** survives: the spec and the benchmark no longer bound the 10× ratio and
   no longer claim sub-millisecond. A `grep` over the whole tree WILL still hit dispatch cards
   — this card quotes the defect it describes, and `probe-adoption-evidence.md` records an
   older, wrong target (marked in place at `cf6cb8a`). A record is not a live claim, and those
   hits do not count. Quote the grep and say which hits are which.
2. The bench's printed bounds equal the spec's stated bounds; re-run `-part a` and paste
   the NFR table, including the re-measured size ratios.
3. `"$FS_BIN" script check-specs.sh` exits 0 with both markers rotated to your commit.
4. `go test ./... -count=1`, `go vet ./...`, `gofmt -l .` clean, and
   `TestConcurrentWritersQueue` still passes.
5. The node says which claim moved, what it moved to, and what you did **not** verify.

## Out of scope

The recall behaviour itself — it is correct and reviewed. Do not touch `matchers.go`,
the fusion rule, the tokenizer, or the version. Do not re-run the whole recall
verification; that is a second review's question, not this round's.

## Budget

**One round.** Two files, plus the spec revision in place. Anything beyond that — stop
and report.

## Resources

No worktree and no branch, deliberately: `check-specs.sh` measures the whole project, so
a commit ahead of the lock refuses the `code` entry gate, and a worktree forked from HEAD
would not carry the untracked `AGENTS.md` that the same gate reads. Work in the main
checkout at `/Users/ianluo/Documents/apps/memstore`. One sibling pane is created by
`--deliver` and `fs close` closes it. Verify with `herdr workspace list` that no
workspace was opened under your name.

## Files

store.go
cmd/bench/main.go

The two `.agents/specs/*.md` documents are revised by this change but are deliberately
not declared: the lock convention leaves their marker lines modified, and
`check-deliverable` requires every declared path to be both committed and clean.
