# CLAUDE.md

Guidance for working in this repository.

## What this is

`asgotochanged` is a herdr plugin popup that lists the files the current branch
changed against its base (committed, staged, unstaged and untracked: what a PR
would ship plus what is pending), previews the diff of the file under the
cursor rendered by hunk, and opens it in the editor, coming back to the list
when the editor exits. It is the Go port of `fm`, a zsh + fzf function from the
user's dotfiles, and the terminal twin of the `aschanged` VS Code extension.
Same frame and diff renderer as `asgitlog`, same filtering and lifecycle as the
asgoto pickers.

It only reads the repository. It never stages, commits, stashes or checks
anything out; what it writes is its own: the chosen diff mode, the whitespace
setting and a cache of rendered diffs.

Distributed as a herdr plugin (`herdr plugin install asumaran/asgotochanged`;
the manifest's `[[build]]` runs `scripts/fetch-binary.sh`). Each GitHub Release
attaches the `asgotochanged-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). There is no published library.

## Stack & layout

Go single module, single `package main`, static binary. TUI: Bubble Tea v2 +
bubbles v2 (`textinput`, `viewport`, `key`, `help`), lipgloss v2,
`sahilm/fuzzy` for matching; `creack/pty` + `charmbracelet/x/vt` to run hunk.
The charm v2 modules are imported under their canonical `charm.land/<name>/v2`
paths (the `github.com/charmbracelet/<name>/v2` spelling is rejected by
`go get`). Files are split by concern:

- `main.go` — flags (`-version`, `-dump`, `-query`), `enterPaneCwd`, the
  diff-mode preference, `tea.NewProgram`, `runDump`.
- `git.go` — `loadRepoInfo` / `repoInfo.line`, `resolveBase`, `loadChanges`
  (name-status + untracked + numstat, all NUL-separated), `diffArgs`.
- `filter.go` — fuzzy rows over the paths.
- `match.go` — `findTight`, the fuzzy matcher with one correction: it is
  greedy (first candidate for each rune, left to right), so a query that
  occurs in one piece could still match scattered letters before it. When the
  query occurs whole, that occurrence is the match, for the highlight and for
  the score. The same file in every tool of the family.
- `prompt.go` — the filter input: its prompt (with the tool's name only outside
  herdr's popup), the placeholder, the `(dev)` mark after the counter. The same
  file in every tool of the family.
- `helpfoot.go` — the help at the foot: the key that expands it, its height
  and its lines cut to the width. The same file in every tool of the family.
- `listnav.go` — `listNav`: the keys that move the cursor through a list and
  where each one takes it, group headers skipped. The same file in every tool
  of the family.
- `highlight.go` — `highlight`/`highlightFrom`, `matchOver`, `onSel`,
  `selPad` and the `stSel`/`stMatch` styles: how a match and the selected row
  look. The same file in every tool of the family.
- `pathcells.go` — `pathCells`, `pathTail`, `tailCut`: a path cut to a width
  by its head, its prefix dimmed, its matches marked. The same file in every
  tool that lists paths.
- `frame.go` — the single-frame layout shared by the family: `hline`, `fit`,
  `framed`, `frameHead`, `splitMain`, `scrollPos` and the section rows
  (`mainY`, `listY`, `frameRows`, each with or without the optional context
  line). Copied, not imported: the same file ships in asgotosession, asgotonotes,
  asgotopr and asgotoissues. A pull request only needs to change it here; the
  maintainer ports the change to the other copies.
- `hunk.go` — hunk as the diff renderer, copied from asgitlog: hunk is a
  full-screen TUI with no static output, so it runs on a tall pty behind a
  terminal emulator and the emulated screen is read back as ANSI lines.
  The same `hunk.go` and `hunk_test.go` ship in github.com/asumaran/asgitlog
  (copied, not imported). A pull request only needs to change them here; the
  maintainer ports the change. Only the renderer is shared: the cache and the
  diff modes differ on purpose.
- `preview.go` — the diff as a `tea.Cmd`: cancellable, a partial frame then
  the final one, diff modes.
- `cache.go` — the finished renders on disk between runs, addressed by the
  patch they were made from (scheme copied from asgitlog).
- `split.go` — the divider between the list and the preview: `loadSplit`,
  `saveSplit`, `stepSplit`, `splitWidths`. The file is copied, not imported:
  the same one ships in asgotosession, asgotonotes, asgotopr and asgotoissues (all under
  github.com/asumaran), and there is no shared library. A pull request only
  needs to change it here; the maintainer ports the change to the other copies.
- `ui.go` — the bubbletea model/Update/View, editing through
  `tea.ExecProcess`, mouse, styles, `pathCells`.
- `scripts/pty-check.py` — end-to-end TUI driver (see Testing).

## Build & run

```bash
go build -o asgotochanged .    # plugin runs ./asgotochanged from the repo root
./asgotochanged -dump          # changed files of the current checkout, no TTY
./asgotochanged -dump -query x # matches with scores
go vet ./... && go test ./...
herdr plugin link "$PWD"   # link does NOT run [[build]]; go build yourself
```

Keybinding (user config): `prefix+m` / `ctrl+alt+m` → `plugin_action`
`asumaran.asgotochanged.open` → `scripts/open-pane.sh` → `herdr plugin pane open`.

## Behaviour / decisions

- **Layout**: one rounded frame of sections split by shared edges, the layout
  asgitlog introduced and every picker of the family follows (`frame.go`). A
  context line on top is only for what the rest of the screen cannot say; here
  it is justified, as in asgitlog: which checkout and branch the list is
  about. A checkout path that does not fit loses its head so the branch keeps
  its place (`repoInfo.line`). The edge under it carries the matches/total
  counter and, in brackets, the base (`[vs origin/main]`, asgitlog's scope).
  The main section is list and diff split by a divider; its bottom edge
  carries the list's position on the left and the diff's scroll position on
  the right, each only while its side overflows. Errors and confirmations take
  the help line. The list starts on screen row `listY(true)`, one cell in from
  the left side, which is what the click-to-row math uses.
- **Moving through the list** is the same in every tool of the family and
  comes from `listnav.go` (the same file in each repo): arrows or
  `ctrl+p`/`ctrl+n` a row, `pgup`/`pgdn` a page, `alt+↑`/`alt+↓` or
  `home`/`end` the ends. `home`/`end` are taken from the filter input's caret
  on purpose (`←`/`→` and `ctrl+e` still move it). The preview scrolls with
  `shift+↑`/`shift+↓` only. The keys are listed in the expanded help.
- **The filter input** comes from `prompt.go` (the same file in every tool of
  the family). Inside herdr's popup the prompt is the arrow alone, because the
  pane's title (`[[panes]] title` in the manifest, the tool's name) already
  says which tool it is, and a placeholder says what the filter searches. Run
  on its own the prompt carries the tool's name. A build that is not a release
  says `(dev)` after the counter, on the edge over the input, never inside the
  prompt.
  herdr sets `HERDR_PLUGIN_ENTRYPOINT_ID` for a plugin pane; that is how the
  two cases are told apart.
- **Help**: the line at the foot shows the tool's own actions, `? help` and the
  quit keys; `?` expands it into every key in columns and the main section
  gives way (`helpfoot.go`, the same file in every tool of the family). `?`
  expands only while the filter is empty, otherwise it is text, like `q`;
  `f1` always does; `esc` folds the help before it quits. Moving, scrolling
  and resizing live in the expanded help only, so the folded line stays short
  enough for a narrow popup. A message (error, notice) takes the help's place
  on one line.
- **Filter matches** look the same in every tool of the family and come from
  one place, `highlight.go` (the same file in each repo; it also owns `stSel`
  and `stMatch`): a match is the match color plus an underline on top of the
  style the text already has, and the selected row shows them too. That row
  is never one big `stSel.Render` around styled text, because the reset that
  ends a match would cut the background: every piece is rendered over `stSel`
  (`highlight(s, idx, stSel)`) and `selPad` fills the rest. Do not write a
  local highlighter.
- **Resizable list**: `shift+←/→` move the divider in 5% steps, as in
  asgitlog. The setting is the PREVIEW's share of the width, clamped to
  30-85 and saved as `split-columns` in the state dir; the default is 75
  (list 25%, preview 75%), the same in every picker of the family. Rows
  must degrade for a narrow list instead of truncating their last columns.
- **No preview header**: hunk already heads every file with its path and its
  `+n -m`. Do not add a header above the diff.
- **What is listed**: ONE `git diff --name-status --no-renames
  --diff-filter=ACMTD -z <merge-base>` (merge base to the WORKING TREE, so
  committed + staged + unstaged) plus `git ls-files --others
  --exclude-standard -z` as `?`. `--no-renames` lists a rename as D old + A
  new so every row is a plain pathspec. `-z` everywhere: paths are never
  quoted or escaped. Rows are sorted by path.
- **Base**: `origin/HEAD`, then `origin/{main,master,develop}`, then the local
  names, the same order as `gcm` in the dotfiles and `aschanged`. No fetch.
  No base or no merge base is a load error shown in the list column.
- **cwd**: herdr starts plugin panes in the plugin's own directory and
  describes the invocation in `HERDR_PLUGIN_CONTEXT_JSON`; `enterPaneCwd`
  moves to `focused_pane_cwd` (else `workspace_cwd`). git runs with `-C
  <top>`, so paths are relative to the top whatever the subdirectory.
- **Diff**: `git diff --no-color <merge-base> -- <path>` (untracked: `git diff
  --no-index -- /dev/null <path>`, whose exit status 1 means "there are
  differences", not an error) rendered by hunk in `split` or `unified` mode.
  Without hunk (`ASGOTOCHANGED_HUNK=none`, or not installed) the same diff with
  `--color=always`. delta is deliberately not used: the family renders diffs
  with hunk.
- **Renders are two-stage, and that repaint is kept off the screen**: hunk
  paints the diff first and the syntax highlighting a few hundred
  milliseconds later, so a render reports a `partial` frame (shown, never
  cached) and then the final one through `previewMsg.next`; the final frame
  replaces the partial one keeping the scroll offset. Seeing the colors change
  under the cursor is the thing to avoid, and two mechanisms (both asgitlog's)
  do it. **Rendering ahead**: `prefetch` renders the `prefetchAround` rows on
  each side of the cursor, nearest first, at most `maxPipelines` (3) at once
  (`m.inflight`, key → cancel); it runs again every time a render reports
  back, so the window fills a few at a time and moving through the list shows
  finished diffs. The selection never waits for a slot: `cancelFarthest` gives
  up a render ahead for it. **Disk cache** (`cache.go`,
  `~/.cache/asgotochanged/renders`, `ASGOTOCHANGED_NO_CACHE` turns it off): a
  finished render is stored gzipped under the sha256 of hunk's fingerprint
  (binary + config files), width, mode and THE PATCH ITSELF, so an edited file
  simply misses and nothing can go stale; a hit returns the highlighted frame
  at once with no partial before it. Only the first ever render of a patch
  shows the repaint. In memory the key is status, path, width, effective mode
  and file mtime. Plain git renders are instant: no prefetch, no disk cache.
- **Whitespace**: `ctrl+s` turns git's `-w` (`--ignore-all-space`, what
  GitHub's "Hide whitespace" does) on and off for every diff; saved as
  `whitespace` in the state dir. It is a flag of the `git diff` that makes the
  patch, so it does not depend on hunk. The flag is part of the in-memory
  render key; the disk cache is addressed by the patch, which already differs.
  A file with nothing left says `(only whitespace changes)`. While it is on,
  the main section's bottom edge carries `[-w]` before the scroll position
  (`diffEdge`): that edge is the one about the diff; the top one is about the
  list, which `-w` does not change.
- **Diff mode**: auto / side by side / single column on `ctrl+t`, saved in the
  plugin state dir (`HERDR_PLUGIN_STATE_DIR`, standalone
  `~/.config/herdr/asgotochanged-tui`). Auto goes side by side from 120 columns
  of diff area, asgitlog's threshold, except for a file that was only added or
  only deleted (`A`, `D`, `?`): side by side would leave one half empty and
  cut the other at the middle, so those go single column (`fileDiff`). An
  explicit mode is obeyed whatever the file.
- **Editing**: `tea.ExecProcess` hands the terminal to the editor
  (`ASGOTOCHANGED_EDITOR`, else `nvim`, else `$EDITOR`, else `vi`) with the
  absolute path, cwd at the top. When it exits the list is loaded again
  (`reloadCmd`): the edit can change a diff, add or remove rows. The cursor
  stays on the same path (`refilter(true)`). A file that does not exist in the
  work tree (deleted) is never handed over: notice on the help line.
- **A query makes the list a search result**: rows are ranked, best match
  first, and the cursor sits on the first one (`rank` in `rank.go`, the same
  file in every picker of the family). Equal scores keep the list's own
  order, the order of the diff, which is also the order without a query. A score says how
  good the match is and nothing about the length of the text (`match.go`).
  Only paths are matched. Matched indexes from
  `sahilm/fuzzy` are BYTE offsets. With an empty query the cursor stays on the
  row it was on.
- **Keys vs. filter**: every printable key filters, so `q` quits only while
  the filter is empty.
- **Paths** that do not fit lose their head, not their tail (`pathCells`), so
  the file name is always visible; the directory part is dimmed.
- **Mouse**: the wheel follows the pointer, as in asgitlog: over the list
  (`overList`) it moves the cursor through the same code as the arrow keys,
  anywhere else it scrolls the diff. A left click on a list row moves the
  cursor and never opens the editor.
- **Alt screen and mouse mode** are declared per frame in `View()`; there is
  no `tea.WithAltScreen` program option in v2.

## Testing

Unit tests build a real git checkout in a temp dir (local `main` as the base,
a feature branch with modified, added, deleted, pending and untracked files,
a path with a space and one with a non-ASCII name) for `loadChanges`, the base
resolution, the plain-git diff and a cancelled render; the UI tests use
synthetic changes: filtering, the counter, the diff-mode cycle and its
persistence, editing a deleted file, reload keeping the cursor, the frame
geometry, clicks.

For end-to-end verification without a TTY, `scripts/pty-check.py
./asgotochanged` (python3 + `pyte`) spawns the binary on a pty, answers the
terminal queries, replays keystrokes and asserts on pyte-rendered frames, in a
throwaway sandbox (fake `HOME`, a real git checkout, hunk off, a stub as
`ASGOTOCHANGED_EDITOR` that logs the path and appends a line to the file). The
v2 renderer repaints with scroll regions, which pyte ignores, so the driver
forces a full redraw (pty resize + SIGWINCH) before reading a frame.

## Commits & branches

- Conventional Commits: `type(scope): description`.
- Never mention AI tooling in commits, PRs, or any repo-visible text as the
  author of changes.
- Default branch is `main`. Don't commit, tag, or push unless explicitly
  asked (releasing is an explicit, separate request).
- `HANDOFF.md` and `REPORT.md` at the root are local notes and are gitignored.

## Releasing

`scripts/release.sh <X.Y.Z>` — clean-tree + vet/build/test gate, CHANGELOG
generation from commit subjects, manifest version sync, commit + tag + GitHub
release; CI (`.github/workflows/release.yml`) attaches
the `asgotochanged-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). Releasing never touches the linked plugin's
`./asgotochanged`; rebuild locally to keep testing dev code.
