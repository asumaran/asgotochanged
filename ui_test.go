package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func stripANSI(s string) string { return ansi.Strip(s) }

// footOf is the line at the foot as the frame draws it.
func footOf(m model) string { return footLine(m.flash, m.notice, m.help, m.keys, m.width-4) }

func press(m model, keys ...tea.KeyPressMsg) model {
	for _, k := range keys {
		next, _ := m.Update(k)
		m = next.(model)
	}
	return m
}

func typed(s string) []tea.KeyPressMsg {
	var out []tea.KeyPressMsg
	for _, r := range s {
		out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return out
}

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
	keyCtrlT = tea.KeyPressMsg{Code: 't', Mod: tea.ModCtrl}
	keyCtrlS = tea.KeyPressMsg{Code: 's', Mod: tea.ModCtrl}
	keyCtrlY = tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl}
	keyPanel = tea.KeyPressMsg{Code: tea.KeyF1}
	keySpace = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}

	keyShiftLeft  = tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModShift}
	keyShiftRight = tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModShift}
)

func fixture(t *testing.T) model {
	t.Helper()
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	ch := changes{base: "origin/main", mergeBase: "abc", files: []changedFile{
		{status: "M", path: "src/cart/total.ts", added: 12, deleted: 3},
		{status: "A", path: "src/cart/tax.ts", added: 40},
		{status: "D", path: "src/cart/old.ts", deleted: 9},
		{status: "?", path: "notes/PLAN.md"},
	}}
	repo := repoInfo{Top: t.TempDir(), Branch: "fix/x", Upstream: "origin/fix/x", Ahead: 2}
	return newModel(repo, ch, "", diffAuto, "", "")
}

func TestFilterNarrowsAndMovesCursor(t *testing.T) {
	m := fixture(t)
	if len(m.rows) != 4 || m.cursor != 0 {
		t.Fatalf("rows = %d, cursor = %d", len(m.rows), m.cursor)
	}
	m = press(m, typed("tax")...)
	if len(m.rows) != 1 || m.current().path != "src/cart/tax.ts" {
		t.Fatalf("filtered rows = %d, current = %+v", len(m.rows), m.current())
	}
	if c, s := ansi.Strip(m.counter()), ansi.Strip(m.status()); c != "1/4" || s != "[vs origin/main]" {
		t.Errorf("counter = %q, status = %q", c, s)
	}
}

// TestFilterRanksAndMovesCursor: under a query the list is a search result,
// best match first with the cursor on it (rank.go); without one, and again
// once the query is gone, the files keep the order of the diff.
func TestFilterRanksAndMovesCursor(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	ch := changes{base: "origin/main", files: []changedFile{
		{status: "M", path: "docs/pull-and-rebase.md"}, // scattered
		{status: "M", path: "docs/explanation.md"},     // inside a word
		{status: "M", path: "notes/plan.md"},
	}}
	paths := func(m model) string {
		var out []string
		for _, r := range m.rows {
			out = append(out, r.f.path)
		}
		return strings.Join(out, ",")
	}
	inOrder := "docs/pull-and-rebase.md,docs/explanation.md,notes/plan.md"
	m := press(newModel(repoInfo{Top: t.TempDir(), Branch: "main"}, ch, "", diffAuto, "", ""), keyDown)
	if got := paths(m); got != inOrder {
		t.Fatalf("no query: rows = %s, want the order of the diff", got)
	}
	m = press(m, typed("plan")...)
	if got := paths(m); got != "notes/plan.md,docs/explanation.md,docs/pull-and-rebase.md" {
		t.Errorf("rows = %s, want the best match first and the scattered one last", got)
	}
	if m.cursor != 0 || m.current().path != "notes/plan.md" {
		t.Errorf("the cursor should sit on the best match: cursor = %d", m.cursor)
	}
	for i := 1; i < len(m.rows); i++ {
		if m.rows[i].score > m.rows[i-1].score {
			t.Errorf("rows are not ranked: %s", paths(m))
		}
	}
	for range "plan" {
		m = press(m, tea.KeyPressMsg{Code: tea.KeyBackspace})
	}
	if got := paths(m); got != inOrder {
		t.Errorf("query cleared: rows = %s, want the order of the diff again", got)
	}
}

// TestEmptyListSaysWhy: a query that matches nothing says so in the list, as
// in every tool of the family (emptyList in listnav.go). TestNothingChanged
// has the tool's own reason.
func TestEmptyListSaysWhy(t *testing.T) {
	m := press(fixture(t), typed("zzzzqq")...)
	if len(m.rows) != 0 {
		t.Fatalf("the query should match nothing, got %d rows", len(m.rows))
	}
	list := ansi.Strip(m.listLines()[0])
	if !strings.HasPrefix(list, " No matches") {
		t.Errorf("the list should say there are no matches: %q", list)
	}
}

// TestEditorArgv: ASGOTOCHANGED_OPENER is a command line and replaces the
// editor; without it and without nvim, $EDITOR is one too, and vi is the last
// resort.
func TestEditorArgv(t *testing.T) {
	t.Setenv("ASGOTOCHANGED_OPENER", "  code  -n -w ")
	if got := editorArgv(); strings.Join(got, "|") != "code|-n|-w" {
		t.Errorf("opener: %q", got)
	}
	t.Setenv("ASGOTOCHANGED_OPENER", "")
	t.Setenv("PATH", t.TempDir()) // no nvim to find
	t.Setenv("EDITOR", "emacs -nw")
	if got := editorArgv(); strings.Join(got, "|") != "emacs|-nw" {
		t.Errorf("$EDITOR: %q", got)
	}
	t.Setenv("EDITOR", "")
	if got := editorArgv(); strings.Join(got, "|") != "vi" {
		t.Errorf("last resort: %q", got)
	}
}

// TestEnterStartsTheEditor: enter on a file that exists hands the terminal to
// the editor, with nothing to say on the help line. The command is never run
// here (a tea.Cmd only runs when the program runs it); when the editor gives
// the terminal back the list is reloaded, and its error is the notice.
func TestEnterStartsTheEditor(t *testing.T) {
	t.Setenv("ASGOTOCHANGED_OPENER", "/usr/bin/true")
	m := fixture(t)
	path := filepath.Join(m.repo.Top, "src", "cart", "total.ts")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("export {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	next, cmd := m.Update(keyEnter)
	m = next.(model)
	if cmd == nil || m.notice != "" {
		t.Fatalf("cmd nil = %v, notice = %q", cmd == nil, m.notice)
	}
	if m.current().path != "src/cart/total.ts" || m.ti.Value() != "" {
		t.Errorf("enter moved the cursor or typed: %q, filter %q", m.current().path, m.ti.Value())
	}
	next, cmd = m.Update(editedMsg{})
	if m = next.(model); cmd == nil || m.notice != "" {
		t.Errorf("back from the editor: reload nil = %v, notice = %q", cmd == nil, m.notice)
	}
	next, _ = m.Update(editedMsg{err: os.ErrNotExist})
	if m = next.(model); !strings.Contains(m.notice, "editor: ") {
		t.Errorf("a failed editor should be the notice: %q", m.notice)
	}
}

func TestEnterOnDeletedFileStays(t *testing.T) {
	m := press(fixture(t), keyDown, keyDown)
	next, cmd := m.Update(keyEnter)
	m = next.(model)
	if cmd != nil || !strings.Contains(m.notice, "does not exist") {
		t.Errorf("cmd nil = %v, notice = %q", cmd == nil, m.notice)
	}
	if help := ansi.Strip(footOf(m)); !strings.Contains(help, "nothing to edit") {
		t.Errorf("footer = %q", help)
	}
}

func TestDiffModeCyclesAndPersists(t *testing.T) {
	m := fixture(t)
	if m = press(m, keyCtrlT, keyCtrlT, keyCtrlT); !strings.Contains(ansi.Strip(footOf(m)), "no renderer found: plain git colors") {
		t.Errorf("with neither renderer the mode changes nothing to see: %q", ansi.Strip(footOf(m)))
	}
	m.deltaBin = "/nonexistent/delta"
	for _, want := range []string{diffSBS, diffSingle, diffAuto} {
		m = press(m, keyCtrlT)
		if m.diffMode != want || loadDiffMode() != want {
			t.Errorf("mode = %q, saved = %q, want %q", m.diffMode, loadDiffMode(), want)
		}
	}
	if !strings.Contains(ansi.Strip(footOf(m)), "diff: auto") {
		t.Errorf("footer = %q, want the confirmation", ansi.Strip(footOf(m)))
	}
}

func TestQQuitsOnlyWithEmptyFilter(t *testing.T) {
	_, cmd := fixture(t).Update(typed("q")[0])
	if cmd == nil {
		t.Error("q with an empty filter should quit")
	}
	m := press(fixture(t), typed("xq")...)
	if m.ti.Value() != "xq" {
		t.Errorf("filter = %q, want q typed as text", m.ti.Value())
	}
}

func TestReloadKeepsTheCursor(t *testing.T) {
	m := press(fixture(t), keyDown, keyDown, keyDown) // notes/PLAN.md
	ch := m.ch
	ch.files = append([]changedFile{{status: "A", path: "a-new-first.ts"}}, ch.files...)
	next, _ := m.Update(reloadedMsg{ch: ch})
	m = next.(model)
	if len(m.rows) != 5 || m.current().path != "notes/PLAN.md" {
		t.Errorf("rows = %d, current = %+v", len(m.rows), m.current())
	}
}

// TestFrameGeometry pins the single-frame layout: exactly height lines, each
// exactly width cells, sections where the click math expects them.
func TestFrameGeometry(t *testing.T) {
	for _, size := range [][2]int{{94, 24}, {150, 16}, {61, 12}} {
		next, _ := fixture(t).Update(tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		m := next.(model)
		lines := strings.Split(m.render(), "\n")
		if len(lines) != size[1] {
			t.Errorf("%v: %d lines, want %d", size, len(lines), size[1])
		}
		for i, l := range lines {
			if w := ansi.StringWidth(l); w != size[0] {
				t.Errorf("%v: line %d is %d cells, want %d: %q", size, i, w, size[0], ansi.Strip(l))
			}
		}
		plain := strings.Split(ansi.Strip(m.render()), "\n")
		if !strings.HasPrefix(plain[0], "╭") || !strings.HasPrefix(plain[len(plain)-1], "╰") ||
			!strings.Contains(plain[1], "fix/x -> origin/fix/x") || !strings.Contains(plain[2], "[vs origin/main]") ||
			!strings.Contains(plain[len(plain)-3], "─ 4/4 ─┴") ||
			!strings.Contains(plain[mainY(true)], "┬") || !strings.HasPrefix(plain[listY(true)], "│▌M  ") {
			t.Errorf("%v: frame sections misplaced:\n%s", size, strings.Join(plain, "\n"))
		}
	}
}

func TestClickSelectsRow(t *testing.T) {
	m := fixture(t)
	next, _ := m.Update(tea.MouseClickMsg{X: 3, Y: listY(true) + 1, Button: tea.MouseLeft})
	clicked := next.(model)
	if got := clicked.current().path; got != "src/cart/tax.ts" {
		t.Errorf("click on the second row selected %s", got)
	}
	// The divider, the preview and the frame's own lines select nothing.
	for _, c := range [][2]int{{0, listY(true) + 1}, {m.listW() + 1, listY(true) + 1}, {3, mainY(true)}, {3, 1}} {
		next, _ = m.Update(tea.MouseClickMsg{X: c[0], Y: c[1], Button: tea.MouseLeft})
		clicked = next.(model)
		if got := clicked.current().path; got != "src/cart/total.ts" {
			t.Errorf("click at %v moved the cursor to %s", c, got)
		}
	}
}

func TestNothingChanged(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	next, _ := newModel(repoInfo{Top: "/r", Branch: "main"}, changes{base: "origin/main"}, "", diffAuto, "", "").
		Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m := next.(model)
	if out := ansi.Strip(m.render()); !strings.Contains(out, "No changes vs origin/main") {
		t.Errorf("render:\n%s", out)
	}
}

// hunkFixture is the fixture with a hunk binary set, so renders are planned
// ahead. No command is ever run: a tea.Cmd only runs when the program runs it.
func hunkFixture(t *testing.T) model {
	t.Helper()
	m := fixture(t)
	m.hunkBin = "/nonexistent/hunk"
	for i := 0; i < 12; i++ {
		m.ch.files = append(m.ch.files, changedFile{status: "M", path: "z/file" + string(rune('a'+i)) + ".go"})
	}
	m.applyFilter()
	return m
}

func TestPrefetchIsBounded(t *testing.T) {
	m := hunkFixture(t)
	if cmd := m.updatePreview(); cmd == nil {
		t.Fatal("the selected file must start rendering")
	}
	if len(m.queue.inflight) != 1 {
		t.Fatalf("inflight = %d, want the selection only until it reports", len(m.queue.inflight))
	}
	m.prefetch(m.wanted(m.prevKey))
	if len(m.queue.inflight) != maxPipelines {
		t.Errorf("inflight = %d, want %d", len(m.queue.inflight), maxPipelines)
	}
	// The nearest rows go first: the one below, then (none above row 0) the next.
	for _, i := range []int{1, 2} {
		if !m.queue.running(m.keyOf(m.rows[i].f)) {
			t.Errorf("row %d is not being rendered ahead", i)
		}
	}
}

// The selection never waits behind a prefetch: what is out of its window is
// given up at once, and it starts as soon as one of those reports (a dying
// render holds its slot, so the processes stay bounded).
func TestSelectionTakesTheSlotOfAStaleRender(t *testing.T) {
	m := hunkFixture(t)
	m.updatePreview()
	first := m.prevKey
	m.prefetch(m.wanted(first))
	m.setCursor(12) // far away from everything in flight
	m.updatePreview()
	dying := 0
	for _, p := range m.queue.inflight {
		if p.dying {
			dying++
		}
	}
	if dying != maxPipelines || m.queue.running(m.prevKey) {
		t.Fatalf("%d dying, selection running = %v: want every stale render given up and the selection waiting for a slot", dying, m.queue.running(m.prevKey))
	}
	res, _ := m.Update(previewMsg{key: first, cancelled: true})
	m = res.(model)
	if !m.queue.running(m.prevKey) || len(m.queue.inflight) > maxPipelines {
		t.Errorf("the first slot freed goes to the selection: running = %v, inflight = %d", m.queue.running(m.prevKey), len(m.queue.inflight))
	}
}

func TestFinishedRenderFreesItsSlotAndIsKept(t *testing.T) {
	m := hunkFixture(t)
	m.updatePreview()
	key := m.prevKey
	next, _ := m.Update(previewMsg{key: key, content: "partial", partial: true, next: func() tea.Msg { return nil }})
	m = next.(model)
	if c, _ := m.queue.get(key); c != "partial" || m.queue.settled(key) || !m.queue.running(key) {
		t.Errorf("a partial frame is shown and kept as partial, and its pipeline is still running")
	}
	next, _ = m.Update(previewMsg{key: key, content: "final"})
	m = next.(model)
	if c, _ := m.queue.get(key); c != "final" || !m.queue.settled(key) {
		t.Errorf("render = %q", c)
	}
	if m.queue.running(key) {
		t.Error("the finished render still holds its slot")
	}
	if len(m.queue.inflight) == 0 {
		t.Error("finishing a render should start the ones around the cursor")
	}
}

// A row whose prefetch already delivered its partial frame shows it when it
// is selected, and a renderer that dies after that frame leaves it in place.
func TestPartialRenderIsShownAndSurvivesAFailure(t *testing.T) {
	m := hunkFixture(t)
	m.updatePreview()
	m.prefetch(m.wanted(m.prevKey))
	ahead := m.keyOf(m.rows[1].f)
	res, _ := m.Update(previewMsg{key: ahead, content: "partial of row 1", partial: true, next: func() tea.Msg { return nil }})
	m = res.(model)
	m.setCursor(1)
	m.updatePreview()
	if got := ansi.Strip(m.prevVP.View()); !strings.Contains(got, "partial of row 1") {
		t.Fatalf("the partial frame of the row rendered ahead must show:\n%s", got)
	}
	res, _ = m.Update(previewMsg{key: ahead, err: errors.New("hunk died")})
	m = res.(model)
	if got := ansi.Strip(m.prevVP.View()); !strings.Contains(got, "partial of row 1") || strings.Contains(got, "hunk died") {
		t.Errorf("what was drawn stays when the renderer dies on the way:\n%s", got)
	}
}

// A render that fails says so in the preview, in the error color, and is not
// tried again on every move.
func TestFailedRenderIsShownAndNotRetried(t *testing.T) {
	m := hunkFixture(t)
	m.updatePreview()
	key := m.prevKey
	res, _ := m.Update(previewMsg{key: key, err: errors.New("fatal: bad object")})
	m = res.(model)
	if got := ansi.Strip(m.prevVP.View()); !strings.Contains(got, "fatal: bad object") {
		t.Fatalf("the failure must show:\n%s", got)
	}
	m.updatePreview()
	if m.queue.running(key) {
		t.Errorf("a failed render is not started again")
	}
}

// Changing how diffs are made gives up the renders of the old setting instead
// of letting them finish and be kept.
func TestOptionChangeCancelsTheOldRenders(t *testing.T) {
	m := hunkFixture(t)
	m.updatePreview()
	m.prefetch(m.wanted(m.prevKey))
	old := m.prevKey
	m.setOption("whitespace", 1)
	if p := m.queue.inflight[old]; p == nil || !p.dying {
		t.Errorf("the render of the old setting must be given up: %+v", p)
	}
}

func TestNoPrefetchWithoutHunk(t *testing.T) {
	m := fixture(t)
	m.updatePreview()
	if m.prefetch(m.wanted(m.prevKey)) != nil || len(m.queue.inflight) != 1 {
		t.Errorf("plain git renders are instant: nothing to render ahead (inflight = %d)", len(m.queue.inflight))
	}
}

func TestAutoGoesSingleColumnForOneSidedFiles(t *testing.T) {
	wide := autoSBSMinW + 40
	for status, want := range map[string]string{"M": diffSBS, "T": diffSBS, "A": diffSingle, "D": diffSingle, "?": diffSingle} {
		if got := fileDiff(diffAuto, wide, changedFile{status: status}); got != want {
			t.Errorf("auto, %s: %q, want %q", status, got, want)
		}
	}
	// An explicit mode is the user's call, whatever the file.
	if got := fileDiff(diffSBS, wide, changedFile{status: "A"}); got != diffSBS {
		t.Errorf("explicit side by side on an added file: %q", got)
	}
}

// TestResizeList: shift+arrows move the divider, the frame still fits, and
// the position is there for the next run.
func TestResizeList(t *testing.T) {
	next, _ := fixture(t).Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m := next.(model)
	w := m.listW()

	m = press(m, keyShiftRight)
	if m.listW() <= w || m.split != splitDefault-splitStep || loadSplit(stateDir()) != m.split {
		t.Errorf("grow: list %d -> %d, split=%d, saved=%d", w, m.listW(), m.split, loadSplit(stateDir()))
	}
	if m.listVP.Width() != m.listW() || m.prevVP.Width() != m.prevW() {
		t.Errorf("viewports %d | %d, want %d | %d", m.listVP.Width(), m.prevVP.Width(), m.listW(), m.prevW())
	}
	for i, l := range strings.Split(m.render(), "\n") {
		if got := ansi.StringWidth(l); got != 120 {
			t.Errorf("line %d is %d cells after the resize", i, got)
		}
	}

	m = press(m, keyShiftLeft, keyShiftLeft)
	if m.listW() >= w || m.split != splitDefault+splitStep {
		t.Errorf("shrink: list %d -> %d, split=%d", w, m.listW(), m.split)
	}
	for range 10 {
		m = press(m, keyShiftLeft)
	}
	if m.split != splitMax {
		t.Errorf("split should clamp at %d, got %d", splitMax, m.split)
	}
}

// TestWhitespaceToggle: ctrl+s flips git's -w, says so, asks for another
// render and is there for the next run.
func TestWhitespaceToggle(t *testing.T) {
	next, _ := fixture(t).Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m := next.(model)
	before := m.prevKey
	m = press(m, keyCtrlS)
	if !m.ignoreWS || !loadIgnoreWS() || m.prevKey == before || !strings.Contains(ansi.Strip(m.render()), "whitespace: ignored") {
		t.Errorf("ignoreWS=%v saved=%v key %q -> %q", m.ignoreWS, loadIgnoreWS(), before, m.prevKey)
	}
	// The confirmation is cleared by a timer; the mark on the diff's edge stays.
	plain := strings.Split(ansi.Strip(m.render()), "\n")
	if edge := plain[len(plain)-3]; !strings.Contains(edge, "┴") || !strings.Contains(edge, "[-w]") {
		t.Errorf("the bottom edge should carry [-w]: %q", edge)
	}
	m = press(m, keyCtrlS)
	if strings.Contains(ansi.Strip(m.render()), "[-w]") {
		t.Error("the mark goes away with the setting")
	}
	m = press(m, keyCtrlS, keyCtrlS)
	if m.ignoreWS || loadIgnoreWS() {
		t.Errorf("second press: ignoreWS=%v saved=%v key=%q", m.ignoreWS, loadIgnoreWS(), m.prevKey)
	}
}

// TestMouseWheelFollowsThePointer: over the list the wheel moves the
// selection, as in asgitlog; anywhere else it scrolls the preview.
func TestMouseWheelFollowsThePointer(t *testing.T) {
	next, _ := fixture(t).Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m := next.(model)
	wheel := func(x int, b tea.MouseButton) {
		next, _ := m.Update(tea.MouseWheelMsg{X: x, Y: listY(true), Button: b})
		m = next.(model)
	}
	first := m.cursor
	wheel(2, tea.MouseWheelDown)
	if m.cursor <= first {
		t.Errorf("wheel down over the list: cursor %d -> %d", first, m.cursor)
	}
	wheel(2, tea.MouseWheelUp)
	if m.cursor != first {
		t.Errorf("wheel up over the list: cursor = %d, want %d", m.cursor, first)
	}
	m.prevVP.SetContent(strings.Repeat("line\n", 200))
	wheel(m.listW()+10, tea.MouseWheelDown)
	if m.cursor != first || m.prevVP.YOffset() == 0 {
		t.Errorf("wheel over the preview: cursor = %d, preview at %d", m.cursor, m.prevVP.YOffset())
	}
}

// TestCopyKeyCopiesThePath covers ctrl+y: the path of the file under the
// cursor goes to the clipboard as the list shows it, the help line confirms it
// for a moment, and the filter is left alone.
func TestCopyKeyCopiesThePath(t *testing.T) {
	log := filepath.Join(t.TempDir(), "clip")
	stub := filepath.Join(t.TempDir(), "clipboard")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat > "+log+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASGOTOCHANGED_CLIPBOARD", stub)
	next, _ := fixture(t).Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m := press(next.(model), keyDown)
	res, cmd := m.Update(keyCtrlY)
	if cmd == nil {
		t.Fatal("ctrl+y returned no command")
	}
	res, _ = res.(model).Update(cmd())
	m = res.(model)
	want := m.current().path
	if got, _ := os.ReadFile(log); string(got) != want {
		t.Errorf("the clipboard got %q, want %q, the path under the cursor", got, want)
	}
	plain := strings.Split(ansi.Strip(m.render()), "\n")
	if help := plain[len(plain)-2]; !strings.Contains(help, "copied "+want) {
		t.Errorf("help line = %q, want the confirmation", help)
	}
	if m.ti.Value() != "" {
		t.Errorf("ctrl+y leaked into the filter: %q", m.ti.Value())
	}
	res, _ = m.Update(clearFlashMsg(m.flash.seq))
	plain = strings.Split(ansi.Strip(res.(model).render()), "\n")
	if help := plain[len(plain)-2]; !strings.Contains(help, "type filter") {
		t.Errorf("after the timer the help is back: %q", help)
	}
}

// TestCopyKeyWithNothingUnderTheCursor: an empty list has no path to copy and
// the clipboard command is never run.
func TestCopyKeyWithNothingUnderTheCursor(t *testing.T) {
	log := filepath.Join(t.TempDir(), "clip")
	stub := filepath.Join(t.TempDir(), "clipboard")
	if err := os.WriteFile(stub, []byte("#!/bin/sh\ncat > "+log+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ASGOTOCHANGED_CLIPBOARD", stub)
	next, _ := fixture(t).Update(tea.WindowSizeMsg{Width: 120, Height: 24})
	m := press(next.(model), typed("zzzz")...)
	res, cmd := m.Update(keyCtrlY)
	if cmd == nil {
		t.Fatal("ctrl+y returned no command")
	}
	res, _ = res.(model).Update(cmd())
	plain := strings.Split(ansi.Strip(res.(model).render()), "\n")
	if help := plain[len(plain)-2]; !strings.Contains(help, "nothing to copy") {
		t.Errorf("help line = %q, want %q", help, "nothing to copy")
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("the clipboard command ran with nothing to copy")
	}
}

// TestRendererToggle covers the renderer, which is chosen in the panel: hunk
// and delta take turns, the choice is remembered, and a render made by one is
// not shown for the other.
func TestRendererToggle(t *testing.T) {
	m := hunkFixture(t)
	m.deltaBin = "/nonexistent/delta"
	if tool := m.tool(); tool.name != toolHunk || tool.bin != m.hunkBin {
		t.Fatalf("hunk is the default renderer: %+v", tool)
	}
	before := m.prevKey
	m = press(m, keyPanel, keySpace) // the renderer is the first option
	if tool := m.tool(); tool.name != toolDelta || tool.bin != m.deltaBin {
		t.Errorf("the panel moves to delta: %+v", tool)
	}
	if m.flash.text != "diffs by delta" || loadRenderer() != toolDelta {
		t.Errorf("flash = %q, remembered = %q", m.flash.text, loadRenderer())
	}
	if m.prevKey == before || !strings.Contains(m.prevKey, "|delta|") {
		t.Errorf("the preview key must name the renderer: %q then %q", before, m.prevKey)
	}
	m = press(m, keySpace)
	if m.tool().name != toolHunk || loadRenderer() != toolHunk {
		t.Errorf("again is back on hunk: %+v", m.tool())
	}

	// With hunk gone delta can still be chosen; going back to hunk cannot.
	m.hunkBin = ""
	m = press(m, keySpace)
	if m.flash.text != "diffs by delta" || m.tool().name != toolDelta || loadRenderer() != toolDelta {
		t.Errorf("delta without hunk installed: flash %q, tool %+v", m.flash.text, m.tool())
	}
	m = press(m, keySpace)
	if m.flash.text != "hunk not found" || loadRenderer() != toolDelta {
		t.Errorf("hunk asked for and missing: flash %q, saved %q", m.flash.text, loadRenderer())
	}
}

// TestPanel: f1 lays the options and the keys over a frame that keeps its
// size, takes every key while it is open, and esc closes it before it quits.
// `?` is text for the filter.
func TestPanel(t *testing.T) {
	next, _ := fixture(t).Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m := next.(model)
	closed := strings.Split(stripANSI(m.render()), "\n")
	if !strings.Contains(closed[len(closed)-2], "f1 options") || strings.Contains(closed[len(closed)-2], "^r") {
		t.Errorf("the help line offers the panel and no renderer key: %q", closed[len(closed)-2])
	}

	m = press(m, keyPanel)
	open := strings.Split(stripANSI(m.render()), "\n")
	if len(open) != len(closed) {
		t.Fatalf("the panel changed the frame's height: %d -> %d", len(closed), len(open))
	}
	for i, l := range open {
		if ansi.StringWidth(l) != 120 {
			t.Errorf("line %d is %d cells wide, want 120", i, ansi.StringWidth(l))
		}
	}
	all := strings.Join(open, "\n")
	for _, want := range []string{"╭─ options ", "Options", "▌ Diff renderer", "Diff mode", "^t", "Whitespace", "^s", "Keys", "scroll the diff", "esc close"} {
		if !strings.Contains(all, want) {
			t.Errorf("the panel lacks %q:\n%s", want, all)
		}
	}
	if open[0] != closed[0] || open[len(open)-1] != closed[len(closed)-1] {
		t.Errorf("the frame's own edges should not move")
	}

	// Keys go to the panel, not to the filter or the list.
	cursor := m.cursor
	m = press(m, append(typed("zz"), keyDown, keyDown, keySpace)...)
	if m.ti.Value() != "" || m.cursor != cursor {
		t.Errorf("the panel should take every key: filter %q, cursor %d -> %d", m.ti.Value(), cursor, m.cursor)
	}
	if !m.ignoreWS || !loadIgnoreWS() {
		t.Errorf("space on the third option should ignore the whitespace")
	}
	next, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	m = next.(model)
	if m.panel.open || cmd != nil {
		t.Errorf("esc closes the panel and nothing else: open=%v cmd=%v", m.panel.open, cmd)
	}

	m = press(m, typed("why?")...)
	if m.ti.Value() != "why?" || m.panel.open {
		t.Errorf("? is text: filter %q, panel open %v", m.ti.Value(), m.panel.open)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyF1})
	if !m.panel.open {
		t.Errorf("f1 opens the panel whatever the filter says")
	}
}

// TestPasteFilters: a paste changes the query without a key press, and the
// list must follow it (toInput). A key that leaves the query alone must not
// move the cursor off the row it is on.
func TestPasteFilters(t *testing.T) {
	m := fixture(t)
	res, _ := m.Update(tea.PasteMsg{Content: "zzzzqq"})
	m = res.(model)
	if m.ti.Value() != "zzzzqq" || len(m.rows) != 0 {
		t.Fatalf("a paste should filter: query %q, %d rows", m.ti.Value(), len(m.rows))
	}
	for range "zzzzqq" {
		res, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m = res.(model)
	}
	if len(m.rows) < 2 {
		t.Skipf("the fixture lists %d rows", len(m.rows))
	}
	res, _ = m.Update(tea.KeyPressMsg{Code: 'a', Text: "a"})
	m = res.(model)
	if len(m.rows) < 2 {
		t.Skipf("the query leaves %d rows", len(m.rows))
	}
	res, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = res.(model)
	at := m.cursor
	res, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
	m = res.(model)
	if m.cursor != at {
		t.Errorf("a key that does not edit the query moved the cursor: %d -> %d", at, m.cursor)
	}
	m.panel.open = true
	res, _ = m.Update(tea.PasteMsg{Content: "xx"})
	if got := res.(model).ti.Value(); got != "a" {
		t.Errorf("a paste under the panel should be dropped, the query is %q", got)
	}
}

// TestGivenUpRenderIsStartedAgain covers an A, B, A selection: the render of A
// that was given up keeps its slot and its key until it reports, so a second
// one cannot be started under the same key and then killed by that late
// report; and once it has reported, the selection's render starts again
// instead of the row saying "rendering…" for good.
func TestGivenUpRenderIsStartedAgain(t *testing.T) {
	m := hunkFixture(t)
	m.updatePreview()
	key := m.prevKey
	if !m.queue.running(key) || key == "" {
		t.Fatalf("the selection's render should be running: %q", key)
	}
	m.queue.cancelStale(nil) // give every render up, the selection's too
	m.updatePreview()
	if p := m.queue.inflight[key]; p == nil || !p.dying {
		t.Errorf("a dying render keeps its key until it reports: %+v", p)
	}
	res, _ := m.Update(previewMsg{key: key, cancelled: true})
	m = res.(model)
	if p := m.queue.inflight[key]; p == nil || p.dying {
		t.Errorf("after the late report the selection's render starts again: %+v", p)
	}
}

// TestMain sandboxes the state dir: tests must never touch the real one, even
// one that forgets to set it.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "asgotochanged-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("HERDR_PLUGIN_STATE_DIR", dir)
	os.Setenv("XDG_CACHE_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// TestRunDump covers -dump: the repo line, the base with the file count, then
// a line per changed file in the order of the diff, with its status and its
// stat.
func TestRunDump(t *testing.T) {
	m := fixture(t)
	var out bytes.Buffer
	runDump(&out, m.repo, m.ch, "", time.Millisecond)
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want the 2 of the summary and the 4 files:\n%s", len(lines), out.String())
	}
	if lines[0] != m.repo.String() || !strings.HasSuffix(lines[0], "  fix/x -> origin/fix/x (ahead 2, behind 0)") {
		t.Errorf("repo line %q", lines[0])
	}
	if want := "base: origin/main (merge base abc), 4 files, loaded in 1ms"; lines[1] != want {
		t.Errorf("base line %q, want %q", lines[1], want)
	}
	for i, want := range [][2]string{
		{"M  src/cart/total.ts ", " +12 -3"},
		{"A  src/cart/tax.ts ", " +40 -0"},
		{"D  src/cart/old.ts ", " +0 -9"},
		{"?  notes/PLAN.md", ""}, // untracked: no stat
	} {
		l := strings.TrimRight(lines[i+2], " ")
		if !strings.HasPrefix(l, want[0]) || !strings.HasSuffix(l, want[1]) || (want[1] == "" && l != want[0]) {
			t.Errorf("file row %q, want %q ... %q", lines[i+2], want[0], want[1])
		}
	}
}

// TestRunDumpQuery covers -dump -query: the matches with their scores, best
// first, instead of the list.
func TestRunDumpQuery(t *testing.T) {
	m := fixture(t)
	var out bytes.Buffer
	runDump(&out, m.repo, m.ch, "src ts", time.Millisecond)
	got := out.String()
	summary, matches, ok := strings.Cut(got, "query \"src ts\":\n")
	if !ok || strings.Count(summary, "\n") != 2 || !strings.Contains(summary, ", 4 files, ") {
		t.Fatalf("want the summary and the query line on top:\n%s", got)
	}
	lines := strings.Split(strings.TrimRight(matches, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d matches, want the 3 files under src:\n%s", len(lines), got)
	}
	last := 0
	for i, l := range lines {
		f := strings.Fields(l)
		if len(f) != 3 || !strings.HasPrefix(f[2], "src/cart/") || strings.Count(matches, " "+f[2]+"\n") != 1 {
			t.Fatalf("match %q, want a score, the status and a path under src, once", l)
		}
		n, err := strconv.Atoi(f[0])
		if err != nil || (i > 0 && n > last) {
			t.Errorf("score %q after %d, want the best first:\n%s", f[0], last, got)
		}
		last = n
	}
	// The matches replace the list: no file that does not match, no stats.
	for _, not := range []string{"PLAN.md", "+12 -3", "+40 -0"} {
		if strings.Contains(got, not) {
			t.Errorf("the query dump has %q, a piece of the full listing:\n%s", not, got)
		}
	}
}
