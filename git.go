package main

// Everything that asks git: the repository summary for the context line, the
// base the branch is compared against, and the files it changed. One diff
// from the merge base to the working tree covers committed, staged and
// unstaged changes; untracked files are appended as "?". It is what a PR
// would ship plus what is still pending, like the aschanged VS Code
// extension.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

func runGit(ctx context.Context, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, "git", args...).Output()
	return strings.TrimRight(string(out), "\n"), err
}

func insideWorkTree() bool {
	out, err := runGit(context.Background(), "rev-parse", "--is-inside-work-tree")
	return err == nil && out == "true"
}

// repoInfo is the context line: which checkout and branch the list is about.
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

func homeRel(p string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		if p == h {
			return "~"
		}
		if strings.HasPrefix(p, h+"/") {
			return "~" + strings.TrimPrefix(p, h)
		}
	}
	return p
}

// resolveBase finds the branch the current one is compared against, like gcm
// in the dotfiles zshrc and aschanged: origin/HEAD first, then
// origin/{main,master,develop}, then the local branches. No fetch is done, so
// origin/<base> is as fresh as the last fetch.
func resolveBase(ctx context.Context) (string, error) {
	if b, err := runGit(ctx, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD"); err == nil && b != "" {
		return b, nil
	}
	for _, c := range []string{"main", "master", "develop"} {
		for _, ref := range []string{"origin/" + c, c} {
			if _, err := runGit(ctx, "rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
				return ref, nil
			}
		}
	}
	return "", fmt.Errorf("no base branch found")
}

// changedFile is one row: a path and what happened to it.
type changedFile struct {
	status  string // A, C, M, T, D, or ? for untracked
	path    string // relative to the top of the work tree
	added   int
	deleted int
	binary  bool
}

// changes is what the list shows and what the previews are rendered against.
type changes struct {
	base      string // e.g. origin/main
	mergeBase string
	files     []changedFile
}

// loadChanges lists the files changed since the merge base, by path.
// --no-renames lists a rename as D old + A new so every path is a plain
// pathspec for the preview. Paths come NUL-separated, so nothing is quoted.
func loadChanges(ctx context.Context) (changes, error) {
	var ch changes
	var err error
	if ch.base, err = resolveBase(ctx); err != nil {
		return ch, err
	}
	if ch.mergeBase, err = runGit(ctx, "merge-base", ch.base, "HEAD"); err != nil {
		return ch, fmt.Errorf("no merge base with %s", ch.base)
	}

	top, _ := runGit(ctx, "rev-parse", "--show-toplevel")
	git := func(args ...string) ([]byte, error) {
		cmd := exec.CommandContext(ctx, "git", append([]string{"-C", top}, args...)...)
		return cmd.Output()
	}

	byPath := map[string]*changedFile{}
	out, err := git("diff", "--name-status", "--no-renames", "--diff-filter=ACMTD", "-z", ch.mergeBase)
	if err != nil {
		return ch, err
	}
	fields := bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0})
	for i := 0; i+1 < len(fields); i += 2 {
		p := string(fields[i+1])
		byPath[p] = &changedFile{status: string(fields[i][:1]), path: p}
	}
	if out, err := git("ls-files", "--others", "--exclude-standard", "-z"); err == nil {
		for _, p := range bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0}) {
			if len(p) > 0 && byPath[string(p)] == nil {
				byPath[string(p)] = &changedFile{status: "?", path: string(p)}
			}
		}
	}
	if out, err := git("diff", "--numstat", "--no-renames", "-z", ch.mergeBase); err == nil {
		for _, rec := range bytes.Split(bytes.TrimRight(out, "\x00"), []byte{0}) {
			f := strings.SplitN(string(rec), "\t", 3)
			if len(f) != 3 || byPath[f[2]] == nil {
				continue
			}
			if f[0] == "-" {
				byPath[f[2]].binary = true
				continue
			}
			byPath[f[2]].added, _ = strconv.Atoi(f[0])
			byPath[f[2]].deleted, _ = strconv.Atoi(f[1])
		}
	}

	for _, f := range byPath {
		ch.files = append(ch.files, *f)
	}
	sort.Slice(ch.files, func(i, j int) bool { return ch.files[i].path < ch.files[j].path })
	return ch, nil
}

// diffArgs is the git command that prints one file's diff. An untracked file
// has nothing to be compared with, so it is diffed against /dev/null.
func diffArgs(top, mergeBase string, f changedFile, color bool) []string {
	flag := "--no-color"
	if color {
		flag = "--color=always"
	}
	args := []string{"-C", top, "-c", "core.quotepath=false", "diff", flag}
	if f.status == "?" {
		return append(args, "--no-index", "--", "/dev/null", f.path)
	}
	return append(args, mergeBase, "--", f.path)
}
