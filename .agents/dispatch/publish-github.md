# Card — publish the project: addressable, hermetic, installable

## The defect class

**The project cannot be depended on or installed by anyone, including its author.**
Three separate reasons, all in the packaging rather than the code, and each one blocks
the next.

## Instances under the class

1. **The module path is a local name.** `go.mod` declares `module memstore`, and four
   files import `"memstore"`. Nothing can `go get` that: it is not a path, it is a
   directory name. Found while answering whether another Go module could use this as a
   library — it can, but only through a `replace` naming an absolute directory, which
   `go mod vendor` then records as `memstore v0.0.0 => /Users/ianluo/.../memstore` with
   **no revision** in it.
2. **The dependencies are not vendored, so the hermetic nix build cannot get them.**
   `flake.nix` publishes only `devShells`, and there is no `vendor/`. The daemon that
   runs the build cannot reach a proxy configured only in the client environment, which
   is the finding `skills/src/flagship/flake.nix` already records in its own comment —
   so `vendorHash` is not a choice available on this machine. Vendoring is forced.
   **Measured cost, by the cap: 137 MB across 2032 files** (`modernc.org/libc` 71 MB,
   `modernc.org/sqlite` 57 MB). That is a large first commit and it is the price of the
   hermetic build; the alternative was tried and cannot run here.
3. **The flake publishes no package.** Only `devShells.default` exists, so there is
   nothing for `nix profile install` or a home-manager module to consume.

## The decision — the user's, 2026-10-07

Publish to **`github.com/IanLuo/memstore`** and install it from the flake. The remote
**exists and is empty**: `git ls-remote git@github.com:IanLuo/memstore.git` exits 0 with
no refs, and SSH authenticates as `IanLuo`. The cap pushes *after* this round; pushing is
not your step.

## What to do

1. **Rename the module** to `github.com/IanLuo/memstore` and update every import that
   names `memstore` — `go.mod`, `cmd/memstore/main.go`, `cmd/bench/main.go`,
   `store_test.go`, `matchers_test.go`. `go build ./...` and `go test ./...` must be
   green after; that is the whole acceptance for this step.
2. **Vendor and commit it.** `go mod vendor`. The tree is committed, not gitignored —
   it is what makes the nix build hermetic. Do not add it to `.gitignore`.
3. **Publish the package from the flake.** Add, beside the existing devShell:

   ```nix
   packages = eachSystem (system:
     let pkgs = pkgsFor system; in {
       default = (pkgs.buildGoModule.override { go = pkgs.go_1_27; }) {
         pname = "memstore";
         version = "0.1.0";
         src = ./.;
         vendorHash = null;          # vendored, so no hash and no fetch
         subPackages = [ "cmd/memstore" ];
       };
     });
   ```

   Go names an installed binary after its package directory, and the directory is already
   `cmd/memstore`, so the binary installs as `memstore` with **no `postInstall` rename** —
   unlike flagship, whose directory is `cmd` and has to be moved. Keep `devShells` as it is.
4. **`.gitignore`** — `/result` (a bare `nix build` leaves it, and this project already has
   a gap about a stale copy of a tool sitting in a tree).
5. **README** — replace the 61-byte placeholder. It must say, in this order: what it is,
   the nine verbs with one line each, how to test, how to install from the flake, and the
   honest limits. The limits are not optional and they are these: **recall is lexical** —
   bm25 over a porter-stemmed index, OR-joined, so `gate` reaches `gates` and one matching
   token of five still ranks the row, but **no embedder exists anywhere in this project**,
   so a query with **zero** shared stems returns nothing; **`forget` is one-way** (no verb
   restores it); `schema_version` is 4. A README that promises "search by meaning" would be
   the third time this project wrote a claim its code does not make.
6. **Commit the whole uncommitted record, by name.** More is dirty than the card first said,
   and every byte of it is the cap's own text that must not stay in a working tree. Untracked:
   `.agents/dispatch/nfr-rebase.md` and `.agents/dispatch/publish-github.md`. Modified:
   `.agents/dispatch/probe-adoption-evidence.md` (the cap's marking note on its wrong NFR
   quotation), and `.agents/specs/memory-store.md`, which carries the 1.3× withdrawal and the
   per-revision evidence cite on top of its marker line. **`memory-store-design.md` carries only
   its marker line** — its schema-bump policy clause was committed at `b3e5e0c`, so it needs no
   content commit, and an earlier draft of this item wrongly said it did. **Commit the content
   of what is genuinely uncommitted**, then rotate the two marker lines to that commit and leave
   those two lines dirty, which is the convention. Stage **by name**; never `git add -A`, and
   never `git checkout --`, `restore` or `stash` anything under `.agents/specs/` — those content
   revisions exist nowhere else, and two of them are the only record of findings this session
   disposed of.
7. **Rotate both lock markers** to this commit. Under the policy the spec now states, a
   commit invalidates the lock even when no criterion moves; say so in the decision rather
   than inventing a change. **No acceptance criterion moves in this round** — and say that
   plainly, because a marker that rotates with no clause behind it is still a claim the doc
   was re-read.
8. **Fix the command vendoring made false, in `AGENTS.md`.** Its "How to run / build / test"
   block says `gofmt -l .          # prints nothing when clean`. That was true before this
   round and is false after it: `go vet ./...` and `go test ./...` skip `vendor/` in module
   mode, but `gofmt` is a file walker and does not, so it now lists third-party files.
   Replace the line with `gofmt -l cmd *.go` and keep a short trailing note saying vendor is
   excluded because only `gofmt` walks it. This is a **published** artefact in a round that
   makes the repository public, which is why it is in scope here rather than left stale —
   found by the worker as a question instead of being widened into silently, which was the
   right call. If you have already committed when this arrives, make a **second** commit
   for it and rotate the markers to the final commit; never amend.

## Acceptance — the check this is held to

1. `nix build .#default` then `./result/bin/memstore` runs: paste the output of `--help` or
   `usage` and one real round — `put` a memory, `search` a paraphrase that shares one token
   with it, and get it back.
2. `nix develop` still enters a shell with `go` and `gopls`.
3. `go build ./...`, `go test ./... -count=1` and `go vet ./...` all clean — module mode
   skips `vendor/`, so those three are unaffected by the vendoring — and
   **`gofmt -l cmd *.go` prints nothing.** Write it exactly that way: a bare `gofmt -l .`
   walks `vendor/` and lists third-party files, which is what the worker reported. First-party
   files are clean; nine vendor files are not, and none of them are ours to reformat.
   `TestConcurrentWritersQueue` still passes.
4. `"$FS_BIN" script check-specs.sh` exits 0 with both markers naming your commit.
5. `git status` shows **exactly two** modified files — `.agents/specs/memory-store.md` and
   `.agents/specs/memory-store-design.md` — and the only change in each is its `specs:locked:`
   marker line. Nothing else is modified and nothing is untracked. `git log --stat` shows the
   vendor tree in the commit.
6. Paste the exact commands and their output on the node.

## Out of scope

Pushing to GitHub — the cap does that. The `~/.config/darwin` home-manager change — a
separate decision. A LICENSE file — the user has not chosen one, and picking a licence on
someone's behalf is not a packaging detail. Do not touch the store, the recall behaviour or
the NFR wording. The two specs' **content** is committed this round but **not edited**: the
marker lines are the only thing you change in them.

## Budget

**One round.** If `nix build` fails for a reason outside these files, stop and report the
failure verbatim rather than working around it.

## Resources

No worktree and no branch. `check-specs.sh` measures the whole project, so a commit ahead of
the lock refuses the `code` entry gate, and a worktree forked from HEAD would not carry the
untracked `AGENTS.md` and `nfr-rebase.md` that the same gate and this card need. Work in the
main checkout. One sibling pane is created by `--deliver`; `fs close` closes it. Verify with
`herdr workspace list` that no workspace was opened under your name.

## Files

go.mod
go.sum
flake.nix
flake.lock
README.md
.gitignore
AGENTS.md
cmd/memstore/main.go
cmd/bench/main.go
store_test.go
matchers_test.go
vendor

Both `.agents/specs/*.md` documents get their markers rotated but are deliberately not
declared: the lock convention leaves them modified, and `check-deliverable` requires every
declared path to be both committed and clean.
