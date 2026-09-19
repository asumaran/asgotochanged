package main

// Paths in a list row. This file is the same in every tool of the family
// that lists paths.

import (
	"unicode/utf8"

	"charm.land/lipgloss/v2"
)

// tailCut fits a path into width cells by dropping its head, never its tail,
// so the last name is always visible. It returns the byte offset the kept
// part starts at, 0 when the whole path fits; a cut path needs a cell for the
// ellipsis, which the caller draws.
func tailCut(path string, width int) (start int) {
	n := utf8.RuneCountInString(path)
	if n <= width || width <= 1 {
		return 0
	}
	drop := n - (width - 1)
	for i := range path {
		if drop == 0 {
			return i
		}
		drop--
	}
	return len(path)
}

// pathTail is the plain text of a path cut to width, for where the caller
// styles it as part of something else.
func pathTail(path string, width int) string {
	if start := tailCut(path, width); start > 0 {
		return "…" + path[start:]
	}
	return path
}

// pathCells renders a path cut to width. The bytes before dimUntil (a root
// prefix, a directory, or the whole path) are dimmed; on the selected row
// everything takes its background instead. The bytes at idx are marked as
// matches over either.
func pathCells(path string, dimUntil int, idx []int, width int, selected bool) string {
	style := func(dimmed bool) lipgloss.Style {
		switch {
		case selected:
			return stSel
		case dimmed:
			return stDim
		}
		return lipgloss.NewStyle()
	}
	start := tailCut(path, width)
	out := ""
	if start > 0 {
		out = style(true).Render("…")
	}
	dimUntil = min(max(dimUntil, start), len(path))
	matched := matchSet(idx)
	return out + highlightFrom(path[start:dimUntil], start, matched, style(true)) +
		highlightFrom(path[dimUntil:], dimUntil, matched, style(false))
}
