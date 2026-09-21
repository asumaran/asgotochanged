# CLAUDE.md

Guidance for working in this repository.

## What this is

`asgotochanged` is a herdr plugin popup that lists the files the current branch
changed against its base (committed, staged, unstaged and untracked: what a PR
would ship plus what is pending), previews the diff of the file under the
cursor rendered by hunk (the default) or delta, and opens it in the editor,
coming back to the list when the editor exits. It started as the Go port of a
zsh + fzf function and is the terminal twin of the `aschanged` VS Code
extension.
Same frame and diff renderer as `asgitlog`, same filtering and lifecycle as the
asgoto pickers.

It only reads the repository. It never stages, commits, stashes or checks
anything out. What it writes is its own: its settings (the panel's options and
the list size) and its cache of rendered diffs.

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

- `main.go`: flags (`-version`, `-dump`, `-query`), the work tree check
  (`fatal` outside one), the saved diff settings, `tea.NewProgram`, `runDump`.
- `git.go`: `loadRepoInfo` / `repoInfo.line`, `resolveBase`, `loadChanges`
  (name-status + untracked + numstat, all NUL-separated), `diffArgs`.
- `filter.go`: fuzzy rows over the paths.
- `match.go`: `findTight`/`tighten`, the fuzzy matcher with one correction: it
  is greedy (first candidate for each rune, left to right), so a query that
  occurs in one piece could still match scattered letters before it. When the
  query occurs whole, that occurrence is the match, for the highlight and for
  the score. `hasTerms` says whether a query searches for anything: spaces and
  a bare `~` or `'` do not, so they never filter, rank or move the cursor. The
  same file in every tool of the family.
- `text.go`: `truncate`, `padRight`, `padLeft`: fitting text, styled or not,
  into cells. The same file in every tool of the family.
- `statedir.go`: `stateDirFor`: the state dir herdr injects
  (`HERDR_PLUGIN_STATE_DIR`) or, when the tool runs on its own, the same
  directory worked out
  (`${XDG_STATE_HOME:-~/.local/state}/herdr/plugins/asumaran.asgotochanged`),
  so the popup and a run from the shell share settings and caches. The same
  file in every tool of the family.
- `setting.go`: `loadSetting`, `saveSetting`: a setting the tool remembers,
  one plain-text file each in the state dir. Every option of the panel is
  kept this way, per tool. The same file in every tool of the family that
  needs it.
- `listmouse.go`: `inList`, `rowUnder`, `wheelKey`: the mouse over the list.
  The wheel goes through the same code as the arrows; a click moves the
  cursor and never opens anything. The same file in every tool of the family.
- `prompt.go`: the filter input: its prompt (with the tool's name only outside
  herdr's popup), the placeholder, the `(dev)` mark on the edge over the
  input. `typeInto` hands a message to the input and reports whether the query
  changed: a key, a terminal paste and the input's own `ctrl+v` all edit it,
  and the caller filters again only when it did. The same file in every tool
  of the family.
- `helpfoot.go`: the help line at the foot, cut to the width, and the key that
  opens the panel. `footLine` is what the foot shows: a flash first, then a
  notice in the error color, else the help. The same file in every tool of the
  family.
- `panel.go`: the panel `f1` opens over the frame: options to change in
  place and every key under them (`option`, `panel`, `panelLines`,
  `overlay`). The same file in every tool of the family.
- `listnav.go`: `listNav`: the keys that move the cursor through a list and
  where each one takes it, group headers skipped. `scrollTo` keeps the cursor
  in view together with the row `withHeader` names: the header of its group
  when that is the row right above. `emptyList` is what a list says instead of
  rows: the error that kept it from loading, in the error color, `No matches`,
  or the tool's own reason. The same file in every tool of the family.
- `highlight.go`: `highlight`/`highlightFrom`, `matchOver`, `onSel`,
  `selPad` and the `stSel`/`stMatch` styles: how a match and the selected row
  look. The same file in every tool of the family.
- `flash.go`: `flash`, `flashMsg`, `clearFlashMsg`: a confirmation that takes
  the help line for a moment. The same file in every tool of the family.
- `clipboard.go`: `copyCmd`: feeds a text to the system clipboard and reports
  it with a `flashMsg`; `ASGOTOCHANGED_CLIPBOARD` replaces the command. The
  same file in every tool of the family.
- `border.go`: `hline`, `framed`, `fit`, `scrollPos`: the primitives the frame
  is drawn with (an edge with texts set into it, a line between the frame's
  sides, the position a scrolled viewport reports on an edge). `fitLines` is
  content as exactly so many lines of a width, and `popupView` is the
  `tea.View` every tool returns: the alt screen and, while the mouse is on,
  cell-motion mouse reports. The same file in every tool of the family.
- `homepath.go`: `tildePath`, `homeDir`, `homeRel`: a path with the home
  directory abbreviated to `~`. The same file in every tool of the family that
  shows paths.
- `fatal.go`: `fatal(tool, msg)`: an error that keeps the tool from starting.
  In herdr's popup the message is held until enter, because the pane closes
  with the process and takes stderr with it; in a shell it is plain stderr and
  exit 1. The same file in every tool of the family that needs it.
- `rank.go`: `rank`: with a query the list is a search result, best score
  first; in a grouped list the groups go by their best item and keep their
  items together, and equal scores keep the list's own order. The same file in
  every tool of the family that ranks its matches.
- `panecwd.go`: `paneDirs`, `paneCwd`, `enterPaneCwd`: which directory a popup
  was opened from. herdr starts a plugin pane in the plugin's own directory
  and hands over `focused_pane_cwd` and `workspace_cwd` in
  `HERDR_PLUGIN_CONTEXT_JSON`; a plain run uses the working directory. The
  same file in every tool of the family that needs it.
- `gitrun.go`: `runGit`: git in the current directory, with git's own stderr
  as the error, and `insideWorkTree`. The same file in every tool of the
  family that needs it.
- `diffmode.go`: how a diff is laid out and fetched: the modes `ctrl+t` walks
  (`effectiveDiff`, `diffLabel`), what the main section's bottom edge says
  about the diff (`diffEdge`), and `limitedOutput`, which runs the command
  that prints it without letting a huge one in (`maxDiffBytes` is each tool's
  own). The same file in every tool of the family that shows diffs.
- `pathcells.go`: `pathCells`, `pathTail`, `tailCut`: a path cut to a width
  by its head, its prefix dimmed, its matches marked. The same file in every
  tool that lists paths.
- `frame.go`: the single-frame layout the pickers share: `frameHead`,
  `splitMain` (list and preview) and the section rows (`mainY`, `listY`,
  `frameRows`, each with or without the optional context line), drawn with the
  primitives of `border.go`. Copied, not imported: the same file ships in
  asgoto, asgotopr, asgotoissues, asgotonotes and asgotosession (all under
  github.com/asumaran), and there is no shared library. A pull request only
  needs to change it here; the maintainer ports the change to the other
  copies.
- `hunk.go`: hunk as the diff renderer, copied from asgitlog: hunk is a
  full-screen TUI with no static output, so it runs on a tall pty behind a
  terminal emulator and the emulated screen is read back as ANSI lines.
  The same `hunk.go` and `hunk_test.go` ship in github.com/asumaran/asgitlog
  (copied, not imported). A pull request only needs to change them here; the
  maintainer ports the change.
- `preview.go`: the diff as a `tea.Cmd`: cancellable, a partial frame then
  the final one, diff modes.
- `rendercache.go`: the rendered diffs kept on disk between runs, addressed by
  an id that cannot go stale (a commit's hash, or `patchID`, a hash of the
  patch itself) plus whatever else changes the output: the renderer, its
  binary and configuration, the width, the mode. The same file asgitlog ships.
- `difftool.go`: what draws a diff: hunk or delta, or git's own colors when
  neither is installed (`diffTool`, `toolBin`, `pickTool`, `renderPatch`).
  `diffPrefs` is the three diff options of the panel (renderer, diff mode,
  whitespace), what each value means and what is flashed about a change. The
  same file asgitlog ships.
- `split.go`: the divider between the list and the preview: `loadSplit`,
  `saveSplit`, `stepSplit`, `splitWidths`, `moveSplit` (one step, remembered)
  and `sizePanes` (the list and the preview get their share of the main
  section). Copied, not imported, like `frame.go`: the same file ships in
  asgotopr, asgotoissues, asgotonotes and asgotosession.
- `ui.go`: the bubbletea model/Update/View, editing through
  `tea.ExecProcess`, mouse, styles.
- `scripts/demo/`: the demo scenario (`scenario.sh` + `keys.json`) that
  `asdemo record` (asumaran/asdemokit, the recording tool shared by the herdr
  plugins) uses to re-record `docs/demo.gif`; see `scripts/demo/README.md`.
- `scripts/pty-check.py`: end-to-end TUI driver (see Testing).

## Build & run

```bash
go build -o asgotochanged .    # plugin runs ./asgotochanged from the repo root
./asgotochanged -dump          # changed files of the current checkout, no TTY
./asgotochanged -dump -query x # matches with scores
go vet ./... && go test ./...
scripts/pty-check.py ./asgotochanged   # end-to-end TUI check on a pty (python3 + pyte)
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
  its place (`repoInfo.line`). The edge under it carries, in brackets, the base
  (`[vs origin/main]`, asgitlog's scope). The main section is list and diff
  split by a divider; its bottom edge carries the matches/total counter under
  the list and, while the diff overflows, its scroll position on the right. Errors and confirmations take
  the help line. The list starts on screen row `listY(true)`, one cell in from
  the left side, which is what the click-to-row math uses.
- **Moving through the list** is the same in every tool of the family and
  comes from `listnav.go` (the same file in each repo): arrows or
  `ctrl+p`/`ctrl+n` a row, `pgup`/`pgdn` a page, `alt+↑`/`alt+↓` or
  `home`/`end` the ends. `home`/`end` are taken from the filter input's caret
  on purpose (`←`/`→` and `ctrl+e` still move it). The preview scrolls with
  `shift+↑`/`shift+↓` only. The keys are listed in the panel.
- **The filter input** comes from `prompt.go` (the same file in every tool of
  the family). Inside herdr's popup the prompt is the arrow alone, because the
  pane's title (`[[panes]] title` in the manifest, the tool's name) already
  says which tool it is, and a placeholder says what the filter searches. Run
  on its own the prompt carries the tool's name. A build that is not a release
  says `(dev)` on the edge over the input, never inside the
  prompt.
  herdr sets `HERDR_PLUGIN_ENTRYPOINT_ID` for a plugin pane; that is how the
  two cases are told apart.
  Whatever reaches the input goes through `toInput`: a key, a paste from the
  terminal (`tea.PasteMsg`) and the input's own `ctrl+v` filter the list the
  same way (`typeInto`), and a message that leaves the query alone (a caret
  move, the blink) never moves the cursor. A paste under the open panel is
  dropped. A query made only of spaces, or a bare `~` or `'`, is not a query
  (`hasTerms`): it does not filter, rank or move the cursor.
- **Help and options**: the line at the foot shows the tool's own actions,
  the panel's key and the quit keys (`helpfoot.go`). `f1` opens the panel (`panel.go`, the same file in
  every tool of the family): the options on top, to change with `←`/`→` or
  `space`, and every key in columns under them, laid out by bubbles' `help`
  from `FullHelp()`. The panel is spliced over the middle of the frame, which
  keeps its size; while it is open it takes every key and the mouse, and `esc`
  closes it before it does anything else. `?` is not a help key: the filter
  has the focus, so it is text. Moving, scrolling and resizing are listed in
  the panel only, so the help line stays short enough for a narrow popup. A
  message (error, notice) takes the help line's place.
  This tool's options are the renderer, the diff mode and the whitespace: `options()` lists them as things stand and
  `setOption` is the one place that changes a setting, for the panel and for
  the keys that kept a shortcut. A setting that is chosen once has no key of
  its own; the panel is where it lives.
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
  names, the same order as `aschanged`. No fetch.
- **Errors**: outside a work tree the tool does not start: the shared `fatal`
  (`fatal.go`) holds the message until enter in the popup and is stderr and
  exit 1 in a shell. No base or no merge base is a load error: it is shown in
  the list in the error color (`loadErr` through the shared `emptyList`), as
  in every tool of the family. Notices (a deleted file, an editor that failed)
  take the help line in the error color until the next key (`footLine`).
- **cwd**: herdr starts plugin panes in the plugin's own directory and
  describes the invocation in `HERDR_PLUGIN_CONTEXT_JSON`; `enterPaneCwd`
  moves to `focused_pane_cwd` (else `workspace_cwd`). git runs with `-C
  <top>`, so paths are relative to the top whatever the subdirectory.
- **Diff**: `git diff --no-color <merge-base> -- <path>` (untracked: `git diff
  --no-index -- /dev/null <path>`, whose exit status 1 means "there are
  differences", not an error) rendered by hunk (`split` or `unified`) or by delta, whichever the panel left
  chosen (`renderer` in the state dir, hunk by default; `pickTool` falls back
  to delta when hunk is missing). With neither (`ASGOTOCHANGED_HUNK=none` and
  `ASGOTOCHANGED_DELTA=none`, or not installed) the same diff with
  `--color=always`. The renderer model (`diffTool`, `toolBin`, `pickTool`,
  `renderPatch`) is `difftool.go`, the same file asgitlog ships. So are the
  three options of the panel (`diffPrefs`): `setOption` hands the change to
  `diffPrefs.set`, which says what to flash. The panel switches to either
  renderer that is installed, delta included while hunk is missing; one that
  is not installed flashes `<name> not found` and changes nothing; with
  neither installed a diff mode change says `no renderer found: plain git
  colors`. Only the option that changed is saved, so a setting never chosen
  stays unset.
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
  up a render ahead for it. **Disk cache** (`rendercache.go`, shared with asgitlog;
  `~/.cache/asgotochanged/renders`, `ASGOTOCHANGED_NO_CACHE` turns it off): a
  finished render is stored gzipped under the sha256 of the renderer's
  fingerprint (binary + configuration), width, mode and an id that here is a
  hash of THE PATCH ITSELF (`patchID`; asgitlog uses the commit), so an edited file
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
- **Copy**: `ctrl+y` copies the path of the file under the cursor, relative to
  the top as the list shows it (`copyCmd` in the shared `clipboard.go`), and
  the help line confirms it for a moment (`flash.go`); it is listed in the
  panel only, the help line has no room left. `ASGOTOCHANGED_CLIPBOARD`
  replaces the clipboard command (the tests point it at a stub).
- **Settings**: the diff mode, the renderer and the whitespace are one file
  each in the state dir (`diff`, `renderer`, `whitespace`; `setting.go`, the
  same file in every tool of the family that remembers an option), next to
  the divider's `split-columns`.
- **Diff mode**: auto / side by side / single column on `ctrl+t`, saved in the
  plugin state dir (`HERDR_PLUGIN_STATE_DIR`, standalone
  `~/.local/state/herdr/plugins/asumaran.asgotochanged`). Auto goes side by side from 120 columns
  of diff area, asgitlog's threshold, except for a file that was only added or
  only deleted (`A`, `D`, `?`): side by side would leave one half empty and
  cut the other at the middle, so those go single column (`fileDiff`). An
  explicit mode is obeyed whatever the file.
- **Editing**: `tea.ExecProcess` hands the terminal to the editor
  (`ASGOTOCHANGED_OPENER`, else `nvim`, else `$EDITOR`, else `vi`) with the
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
`ASGOTOCHANGED_OPENER` that logs the path and appends a line to the file, another
as `ASGOTOCHANGED_CLIPBOARD` that logs what it was fed). The
v2 renderer repaints with scroll regions, which pyte ignores, so the driver
forces a full redraw (pty resize + SIGWINCH) before reading a frame.

## Commits & branches

- Conventional Commits: `type(scope): description` (feat, fix, chore, docs,
  style, refactor, test, perf).
- Never mention AI tooling in commits, PRs, or any repo-visible text as the
  author of changes.
- Default branch is `main`. Don't commit, tag, or push unless explicitly
  asked (releasing is an explicit, separate request).
- `HANDOFF.md` and `REPORT.md` at the root are local notes and are gitignored.

## Releasing

`scripts/release.sh <X.Y.Z>`: clean-tree + vet/build/test gate, CHANGELOG
generation from commit subjects, manifest version sync, commit + tag + GitHub
release; CI (`.github/workflows/release.yml`) attaches
the `asgotochanged-<os>-<arch>` binaries (macOS and Linux, arm64 and amd64). Releasing never touches the linked plugin's
`./asgotochanged`; rebuild locally to keep testing dev code.
