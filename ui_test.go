package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func stripANSI(s string) string { return ansi.Strip(s) }

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

func TestFilterKeepsOrderAndMovesCursor(t *testing.T) {
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

func TestEnterOnDeletedFileStays(t *testing.T) {
	m := press(fixture(t), keyDown, keyDown)
	next, cmd := m.Update(keyEnter)
	m = next.(model)
	if cmd != nil || !strings.Contains(m.notice, "does not exist") {
		t.Errorf("cmd nil = %v, notice = %q", cmd == nil, m.notice)
	}
	if help := ansi.Strip(m.footMsg()); !strings.Contains(help, "nothing to edit") {
		t.Errorf("footer = %q", help)
	}
}

func TestDiffModeCyclesAndPersists(t *testing.T) {
	m := fixture(t)
	for _, want := range []string{diffSBS, diffSingle, diffAuto} {
		m = press(m, keyCtrlT)
		if m.diffMode != want || loadDiffMode() != want {
			t.Errorf("mode = %q, saved = %q, want %q", m.diffMode, loadDiffMode(), want)
		}
	}
	if !strings.Contains(ansi.Strip(m.footMsg()), "diff: auto") {
		t.Errorf("footer = %q, want the confirmation", ansi.Strip(m.footMsg()))
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
	if len(m.inflight) != 1 {
		t.Fatalf("inflight = %d, want the selection only until it reports", len(m.inflight))
	}
	m.prefetch()
	if len(m.inflight) != maxPipelines {
		t.Errorf("inflight = %d, want %d", len(m.inflight), maxPipelines)
	}
	// The nearest rows go first: the one below, then (none above row 0) the next.
	for _, i := range []int{1, 2} {
		f := m.rows[i].f
		if _, ok := m.inflight[previewKey(m.repo.Top, f, m.prevW(), fileDiff(m.diffMode, m.prevW(), f), m.tool())]; !ok {
			t.Errorf("row %d is not being rendered ahead", i)
		}
	}
}

func TestSelectionNeverWaitsForASlot(t *testing.T) {
	m := hunkFixture(t)
	m.updatePreview()
	m.prefetch()
	m.cursor = 12 // far away from everything in flight
	if cmd := m.updatePreview(); cmd == nil {
		t.Fatal("the new selection must start rendering at once")
	}
	if _, ok := m.inflight[m.prevKey]; !ok || len(m.inflight) != maxPipelines {
		t.Errorf("inflight = %d, selected running = %v: want one render given up for the selection", len(m.inflight), ok)
	}
}

func TestFinishedRenderFreesItsSlotAndIsKept(t *testing.T) {
	m := hunkFixture(t)
	m.updatePreview()
	key := m.prevKey
	next, _ := m.Update(previewMsg{key: key, content: "partial", partial: true, next: func() tea.Msg { return nil }})
	m = next.(model)
	if _, cached := m.renders[key]; cached || len(m.inflight) == 0 {
		t.Errorf("a partial frame must be shown, not kept, and its pipeline is still running")
	}
	next, _ = m.Update(previewMsg{key: key, content: "final"})
	m = next.(model)
	if m.renders[key] != "final" {
		t.Errorf("renders[key] = %q", m.renders[key])
	}
	if _, running := m.inflight[key]; running {
		t.Error("the finished render still holds its slot")
	}
	if len(m.inflight) == 0 {
		t.Error("finishing a render should start the ones around the cursor")
	}
}

func TestNoPrefetchWithoutHunk(t *testing.T) {
	m := fixture(t)
	m.updatePreview()
	if m.prefetch() != nil || len(m.inflight) != 1 {
		t.Errorf("plain git renders are instant: nothing to render ahead (inflight = %d)", len(m.inflight))
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

	m.hunkBin = ""
	m = press(m, keySpace)
	if m.flash.text != "hunk not found" || m.tool().name != toolDelta {
		t.Errorf("without hunk there is nothing to switch to: flash %q, tool %+v", m.flash.text, m.tool())
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
