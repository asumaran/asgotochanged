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
