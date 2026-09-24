package main

import (
	"strings"
	"testing"
)

// TestRepoInfoLine: the branch and its upstream keep their place; a checkout
// path that does not fit loses its head, and goes when no readable tail fits.
func TestRepoInfoLine(t *testing.T) {
	ri := repoInfo{Top: "/x/some/deep/checkout/path", Branch: "fix/x", Upstream: "origin/fix/x", Ahead: 2, Behind: 0}
	branch := "fix/x -> origin/fix/x (ahead 2, behind 0)"
	if got := ri.line(0); got != "/x/some/deep/checkout/path  "+branch {
		t.Errorf("line(0) = %q, want the whole summary", got)
	}
	if got := ri.line(len(branch) + 2 + 12); !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "  "+branch) || len([]rune(got)) != len(branch)+2+12 {
		t.Errorf("a path with room loses its head: %q", got)
	}
	if got := ri.line(len(branch) + 6); got != branch {
		t.Errorf("a path without room for a tail goes: %q", got)
	}
	if got := (repoInfo{Top: "/x", Branch: "main"}).line(20); got != "/x  main" {
		t.Errorf("no upstream: %q", got)
	}
}
