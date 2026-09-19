package main

import (
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
	if c := ansi.Strip(m.counter()); c != "1/4 [vs origin/main]" {
		t.Errorf("counter = %q", c)
	}
}

func TestEnterOnDeletedFileStays(t *testing.T) {
	m := press(fixture(t), keyDown, keyDown)
	next, cmd := m.Update(keyEnter)
	m = next.(model)
	if cmd != nil || !strings.Contains(m.notice, "does not exist") {
		t.Errorf("cmd nil = %v, notice = %q", cmd == nil, m.notice)
	}
	if help := ansi.Strip(m.footer()); !strings.Contains(help, "nothing to edit") {
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
	if !strings.Contains(ansi.Strip(m.footer()), "diff: auto") {
		t.Errorf("footer = %q, want the confirmation", ansi.Strip(m.footer()))
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
			!strings.Contains(plain[1], "fix/x -> origin/fix/x") || !strings.Contains(plain[2], "4/4") ||
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

func TestPathCellsKeepsTheFileName(t *testing.T) {
	if got := pathCells("src/components/cart/CartTotal.tsx", nil, 16, false); got != "…t/CartTotal.tsx" {
		t.Errorf("got %q", got)
	}
}

func TestNothingChanged(t *testing.T) {
	t.Setenv("HERDR_PLUGIN_STATE_DIR", t.TempDir())
	m := newModel(repoInfo{Top: "/r", Branch: "main"}, changes{base: "origin/main"}, "", diffAuto, "", "")
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
		if _, ok := m.inflight[previewKey(m.repo.Top, f, m.prevW(), fileDiff(m.diffMode, m.prevW(), f))]; !ok {
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
