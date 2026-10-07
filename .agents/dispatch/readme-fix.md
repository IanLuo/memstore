# Card — a published artefact that demonstrates the opposite of what it claims

## The defect class

**The README is the public face of this round, and its examples do not do what its own
sentences say.** Found by the independent review of `a497de9` (`memstore:t-beec6a55`) as
three `text` findings against the change. Two are in the README, which is the artefact a
stranger reads first, and the round exists to publish it.

## Instances under the class

1. **`README.md` runs a command that cannot pass, and says it checks something it does not.**
   Its test block still has `nix develop --command gofmt -l .`, with a comment claiming
   *"vendor/ is third-party; check first-party files only"*. `vendor/` is now committed, so
   the command prints **9 files, every one of them third-party** (uuid ×7, go-isatty, bigfft)
   and **zero** first-party. The comment asserts a first-party check the command does not
   perform. This is the *identical* defect that card item 8 fixed in `AGENTS.md`; the README
   was written before that defect was known and was missed. Fix: `gofmt -l cmd *.go`.
2. **The README's only usage example returns nothing.** It puts
   `"search returns best first"` and then searches `"how are results ordered"` — the two
   strings share **zero stems**, so the example prints `[]`, directly under a line describing
   search as returning "matching memories, best first". An example that demonstrates a null
   result teaches the reader that the tool is broken. Fix it so it returns the row, and make
   it teach the boundary rather than hide it: keep one query that shares a stem (it returns
   the row) and show one with **no** shared stem returning `[]`, with the sentence that
   explains why — no embedder exists in this project, so zero overlap finds nothing and that
   is the stated limit, not a failure.
3. **`AGENTS.md`'s freshness date is stale.** It reads `Last verified: 2026-10-07` and the
   file was edited on 2026-10-08. Card item 8 named only the `gofmt` line, so this is minor —
   fix the date.

## One riding fix, named and justified

4. **`flake.nix`'s systems list declares a platform nixpkgs has dropped.**
   `x86_64-darwin` was removed in nixpkgs 26.11, so `nix eval .#devShells.x86_64-darwin.default`
   and `.#packages.x86_64-darwin.default` both **throw** rather than evaluate, and
   `nix flake check` cannot pass. The review classified this as *discovered* and pre-existing
   (`devShells.x86_64-darwin` threw at `cf6cb8a` too), so it is not this change's defect — it
   rides because `flake.nix` is a path this round declares, the fix is one line, and the round
   exists to publish a flake. **Drop the dropped system and say why in a comment.** Verify by
   evaluating every remaining system's `devShells.<system>.default.drvPath`.

## Acceptance — the check this is held to

1. **Run every command the README shows, in order, and paste the real output.** A README
   example that was not run is how this defect happened. In particular the search example
   must return the row, and the zero-overlap example must print `[]`.
2. `gofmt -l cmd *.go` prints nothing; `gofmt -l .` prints 9 lines, all `vendor/` — that is
   expected and is why the README must not use the bare form.
3. `nix eval .#devShells.<system>.default.drvPath` for every system still in the list, and
   `nix build .#default` still exits 0 with `./result/bin/memstore` running.
4. `go build ./...`, `go test ./... -count=1`, `go vet ./...` clean.
5. `"$FS_BIN" script check-specs.sh` exits 0 **after** the commit, and `git status --short`
   shows **exactly two** lines — `.agents/specs/memory-store.md` and
   `.agents/specs/memory-store-design.md` — and **nothing untracked**. That requires the
   round's own record to be committed by name in the same commit: `.agents/dispatch/readme-fix.md`
   is untracked and `.agents/dispatch/publish-github.md` was modified by the cap after the last
   round committed it. The `## Files` list below stays at three files **on purpose** — the record
   is bookkeeping, `check-deliverable` measures the deliverable's paths, and the record is
   committed without being declared, exactly as last round. An earlier draft of this criterion
   said "only the two marker lines" and would have been unsatisfiable without the record commit,
   which is how the worker found it.
6. Nothing else changed. This is a text round with one flake line — no store code, no spec
   wording, no version.

## Budget

**One round.** Three files.

## Resources

No worktree and no branch — `check-specs.sh` measures the whole project, so a commit ahead of
the lock refuses the entry gate, and a worktree from HEAD would not carry the untracked state
the gate reads. Work in the main checkout. One sibling pane is created by `--deliver` and
`fs close` closes it. Verify with `herdr workspace list` that no workspace was opened under
your name.

## Files

README.md
AGENTS.md
flake.nix
