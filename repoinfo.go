package main

// The repository summary of the context line: which checkout and branch the
// list is about.
//
// This file is the same in every tool of the family that lists a repository.

import (
	"context"
	"strconv"
	"strings"
)

// repoInfo is the checkout, its branch (the short hash when detached), the
// upstream and how far the two are from each other.
type repoInfo struct {
	Top      string
	Branch   string
	Upstream string
	Ahead    int
	Behind   int
}

func loadRepoInfo() repoInfo {
	ctx := context.Background()
	var ri repoInfo
	ri.Top, _ = runGit(ctx, "rev-parse", "--show-toplevel")
	ri.Branch, _ = runGit(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if ri.Branch == "" {
		ri.Branch, _ = runGit(ctx, "rev-parse", "--short", "HEAD")
	}
	ri.Upstream, _ = runGit(ctx, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if ri.Upstream != "" {
		// --left-right on upstream...HEAD: left = behind, right = ahead.
		if out, err := runGit(ctx, "rev-list", "--left-right", "--count", ri.Upstream+"...HEAD"); err == nil {
			if f := strings.Fields(out); len(f) == 2 {
				ri.Behind, _ = strconv.Atoi(f[0])
				ri.Ahead, _ = strconv.Atoi(f[1])
			}
		}
	}
	return ri
}

func (ri repoInfo) String() string { return ri.line(0) }

// line is the summary fitted into width (0 = as long as it is). The branch
// and its upstream are what change from one popup to the next, so a checkout
// path that does not fit loses its head, not the branch its place.
func (ri repoInfo) line(width int) string {
	branch := ri.Branch
	if ri.Upstream != "" {
		branch += " -> " + ri.Upstream + " (ahead " + strconv.Itoa(ri.Ahead) + ", behind " + strconv.Itoa(ri.Behind) + ")"
	}
	top := homeRel(ri.Top)
	if room := width - 2 - len([]rune(branch)); width > 0 && len([]rune(top)) > room && room > 8 {
		r := []rune(top)
		top = "…" + string(r[len(r)-(room-1):])
	}
	return top + "  " + branch
}
