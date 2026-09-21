# asgotochanged

A [herdr](https://github.com/asumaran/herdr) plugin popup that lists the files
your branch changed against its base, shows the diff of the one under the
cursor, and opens it in your editor. The list is what a PR would ship plus
what is still pending: committed, staged, unstaged and untracked files. It
also runs as a plain command in any git checkout.

Sibling of [asgitlog](https://github.com/asumaran/asgitlog) (same frame, same
diffs, by [hunk](https://hunk.dev) or [delta](https://github.com/dandavison/delta)) and of the asgoto pickers
([asgotopr](https://github.com/asumaran/asgotopr),
[asgotonotes](https://github.com/asumaran/asgotonotes),
[asgotosession](https://github.com/asumaran/asgotosession)).

```
╭──────────────────────────────────────────────────────────────────────────────────────────────────────╮
│ ~/wt/shop/fix-cart-total  fix/cart-total -> origin/fix/cart-total (ahead 2, behind 0)                │
├─────────────────────────────────────────────────────────────────────────────────── [vs origin/main] ─┤
│ ❯ Search by path…                                                                                    │
├───────────────────────────┬──────────────────────────────────────────────────────────────────────────┤
│▌M  src/cart/total.ts      │  src/cart/total.ts                                      +12 -3           │
│ A  src/cart/tax.ts        │ ▌··· 13 unchanged lines ···                                              │
│ D  src/cart/old.ts        │ ▌14 14    const subtotal = items.reduce(sum, 0)                          │
│ ?  notes/PLAN.md          │ ▌15    -  return subtotal                                                │
│                           │ ▌   15 +  return subtotal + tax(subtotal)                                │
├───────────────────── 4/4 ─┴─────────────────────────────────────────────────────────────────── 5/64 ─┤
│ type filter • enter edit • ^t diff mode • ^s whitespace • f1 options • esc/q quit                    │
╰──────────────────────────────────────────────────────────────────────────────────────────────────────╯
```

## Requirements

git and macOS or Linux; herdr >= 0.7.5 for the popup. [hunk](https://hunk.dev) or
[delta](https://github.com/dandavison/delta) render the diffs (`brew install hunk git-delta`); with neither
the diffs are git's own, in color.
Enter opens `nvim`, or `$EDITOR` when there is no nvim.

## Install

```
herdr plugin install asumaran/asgotochanged
```

The manifest's `[[build]]` runs `scripts/fetch-binary.sh`, which downloads the
release binary matching the manifest version and falls back to `go build`
(`ASGOTOCHANGED_BUILD_FROM_SOURCE=1` skips the download).

Bind a key to the `open` action in `~/.config/herdr/config.toml`:

```toml
[[keys.command]]
key = ["prefix+m", "ctrl+alt+m"]
type = "plugin_action"
command = "asumaran.asgotochanged.open"
description = "asgotochanged (branch changes)"
```

The popup works on the repository of the pane that was focused when it opened.

## Usage

The filter input is focused on open, so just type; it matches the paths. A query of several words matches them in any order (`login fix` finds "fix login flow"), and a word starting with `'` must occur as typed instead of fuzzily (`'dex`). One
row per file, by path, with its status:

| status | meaning |
| --- | --- |
| `M` | modified |
| `A` | added (`C` copied) |
| `D` | deleted |
| `T` | type changed (file, symlink, submodule) |
| `?` | untracked |

The top line says which checkout and branch you are looking at. The edge under
it shows how many files match out of the total and, in brackets, the base the
branch is compared against.

| key | action |
| --- | --- |
| `enter` | open the file in the editor; quitting the editor comes back to the list |
| `ctrl+t` | diff mode: auto, side by side, single column (remembered) |
| panel: Diff renderer | render the diffs with hunk or with delta, as in asgitlog (remembered per tool; hunk by default, delta when hunk is not installed) |
| `ctrl+s` | show or ignore whitespace changes, like GitHub's "Hide whitespace" (`git diff -w`, remembered); `[-w]` on the diff's bottom edge while it is on |
| `ctrl+y` | copy the path of the file under the cursor, relative to the repository root as the list shows it; the help line confirms it for a moment |
| `↑/↓`, `ctrl+p`/`ctrl+n` | move the cursor |
| PgDn/PgUp | move the cursor a page |
| `alt+↑`/`alt+↓`, Home/End | top or bottom of the list |
| `shift+↓`/`shift+↑`, mouse wheel over the diff | scroll the diff |
| mouse wheel over the list | move the cursor |
| `f1` | open the panel: the renderer, the diff mode and the whitespace to change in place, and every key (`esc` closes it) |
| `shift+←`/`shift+→` | resize the list; the split is remembered (the list takes a quarter of the width by default) |
| click | select a row |
| `esc`, `q` with an empty filter | quit |

As a command: `asgotochanged [query]`, where `query` is the initial filter.

## Behavior notes

- The base is `origin/HEAD`, then `origin/main`, `origin/master` or
  `origin/develop`, then the same names as local branches. Nothing is fetched,
  so `origin/<base>` is as fresh as your last fetch.
- Every diff is taken from the merge base with the base to the working tree,
  so it covers what is committed on the branch and what is not yet. An
  untracked file is shown whole, as an addition.
- A rename is listed as the old path deleted and the new one added, so every
  row is a plain path.
- After the editor exits the list is read again: an edit can change a diff,
  add a file or make one disappear. The cursor stays on the file it was on.
- A deleted file has nothing to open; the popup says so and stays up.
- Auto goes side by side when the diff area is at least 120 columns wide,
  except for added, deleted and untracked files: one half would be empty.
- hunk draws a diff first and its syntax highlighting a moment later. To keep
  that repaint off the screen, the files around the cursor are rendered ahead
  of time and finished renders are kept in `~/.cache/asgotochanged` between runs
  (`ASGOTOCHANGED_NO_CACHE=1` turns the cache off). You only see the colors come
  in the first time a diff is ever rendered.
- asgotochanged only reads the repository. It never stages, commits or checks
  anything out. What it writes is its own: the diff mode, the whitespace setting and
  that cache.

## Development

```bash
go build -o asgotochanged .   # local build (plugin runs ./asgotochanged from the repo root)
./asgotochanged -dump         # print the changed files of the current checkout (no TTY)
./asgotochanged -dump -query total   # matches with their scores
go vet ./... && go test ./...
scripts/pty-check.py ./asgotochanged   # end-to-end TUI check on a pty (python3 + pyte)
herdr plugin link "$PWD"   # register the working copy (no build step)
```

`ASGOTOCHANGED_OPENER` replaces the editor command; `ASGOTOCHANGED_HUNK` and
`ASGOTOCHANGED_DELTA` replace the renderers' binaries (`none` turns one off). `ASGOTOCHANGED_CLIPBOARD` replaces the
clipboard command `ctrl+y` feeds the path to (`pbcopy` on macOS, else the first
of `wl-copy`, `xclip` and `xsel`); the tests point it at a stub.
`ASGOTOCHANGED_POPUP_WIDTH` / `ASGOTOCHANGED_POPUP_HEIGHT` override the popup
size from the manifest.

## Releasing

`scripts/release.sh <X.Y.Z>` gates on a clean tree + green vet/build/test,
generates the CHANGELOG entry from commit subjects, syncs the manifest
version, commits, tags and publishes the GitHub release; CI then attaches
the `asgotochanged-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64), the assets `fetch-binary.sh` downloads on installs.
