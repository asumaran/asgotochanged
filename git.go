package main

// Everything that asks git: the repository summary for the foot, the
// base the branch is compared against, and the files it changed. One diff
// from the merge base to the working tree covers committed, staged and
// unstaged changes; untracked files are appended as "?". It is what a PR
// would ship plus what is still pending, like the aschanged VS Code
// extension.

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

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
// has nothing to be compared with, so it is diffed against /dev/null. ignoreWS
// is git's -w, what GitHub's "Hide whitespace" does.
func diffArgs(top, mergeBase string, f changedFile, color, ignoreWS bool) []string {
	flag := "--no-color"
	if color {
		flag = "--color=always"
	}
	args := []string{"-C", top, "-c", "core.quotepath=false", "diff", flag}
	if ignoreWS {
		args = append(args, "-w")
	}
	if f.status == "?" {
		return append(args, "--no-index", "--", "/dev/null", f.path)
	}
	return append(args, mergeBase, "--", f.path)
}
