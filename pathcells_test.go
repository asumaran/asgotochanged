package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPathCellsKeepsTheTail(t *testing.T) {
	path := "~/wt/monorepo-front/fix-ESHOP-551"
	if got := ansi.Strip(pathCells(path, len(path), nil, 14, false)); got != "…fix-ESHOP-551" {
		t.Errorf("got %q", got)
	}
	if got := ansi.Strip(pathCells("~/a.md", 0, nil, 20, false)); got != "~/a.md" {
		t.Errorf("a path that fits is untouched: %q", got)
	}
	if got := pathTail(path, 14); got != "…fix-ESHOP-551" || pathTail("~/a.md", 20) != "~/a.md" {
		t.Errorf("pathTail = %q", got)
	}
	if got := ansi.Strip(pathCells("~/añó/b.md", 0, nil, 5, false)); got != "…b.md" || ansi.StringWidth(got) != 5 {
		t.Errorf("the cut counts cells, not bytes: %q", got)
	}
}

func TestPathCellsDimsThePrefixAndMarksMatches(t *testing.T) {
	path := "src/cart/total.ts" // the directory ends at byte 9, where the t of total is
	plain := pathCells(path, 9, []int{9}, 30, false)
	if !strings.HasPrefix(plain, stDim.Render("src/cart/")) || !strings.Contains(plain, stMatch.Render("t")) || !strings.HasSuffix(plain, "otal.ts") {
		t.Errorf("plain = %q", plain)
	}
	sel := pathCells(path, 9, []int{9}, 30, true)
	if !strings.HasPrefix(sel, stSel.Render("src/cart/")) || !strings.Contains(sel, matchOver(stSel).Render("t")) || !strings.HasSuffix(sel, stSel.Render("otal.ts")) {
		t.Errorf("selected = %q", sel)
	}
	// a match in the dimmed part, on a path that lost its head: the offsets
	// still refer to the whole path
	cut := pathCells(path, 9, []int{4}, 14, false)
	if ansi.Strip(cut) != "…cart/total.ts" || ansi.StringWidth(cut) != 14 {
		t.Errorf("cut = %q", ansi.Strip(cut))
	}
	if !strings.HasPrefix(cut, stDim.Render("…")+matchOver(stDim).Render("c")) {
		t.Errorf("the c of cart is marked right after the ellipsis: %q", cut)
	}
}
