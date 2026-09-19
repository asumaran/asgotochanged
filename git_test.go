package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testRepo builds a checkout with a local main (the base, there is no origin)
// and a feature branch that modifies, adds and deletes files, plus an
// untracked one. The process moves into it for the test.
func testRepo(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, text string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q", "-b", "main")
	write("keep.txt", "one\ntwo\n")
	write("old.txt", "gone soon\n")
	write("src/mod file.go", "package a\n")
	git("add", ".")
	git("commit", "-q", "-m", "base")
	git("checkout", "-q", "-b", "feature")
	write("src/mod file.go", "package a\n\nvar x = 1\n")
	write("src/añadido.go", "package a\n")
	git("rm", "-q", "old.txt")
	git("add", ".")
	git("commit", "-q", "-m", "work")
	write("keep.txt", "one\ntwo\nthree\n") // pending, not committed
	write("notes/PLAN.md", "# plan\n")     // untracked

	t.Chdir(dir)
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")
	return dir
}

func TestLoadChanges(t *testing.T) {
	testRepo(t)
	ch, err := loadChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ch.base != "main" || ch.mergeBase == "" {
		t.Errorf("base = %q, merge base = %q", ch.base, ch.mergeBase)
	}
	var got []string
	for _, f := range ch.files {
		got = append(got, f.status+" "+f.path)
	}
	want := "M keep.txt,? notes/PLAN.md,D old.txt,A src/añadido.go,M src/mod file.go"
	if s := strings.Join(got, ","); s != want {
		t.Errorf("files = %s\nwant    %s", s, want)
	}
	for _, f := range ch.files {
		switch f.path {
		case "keep.txt":
			if f.added != 1 || f.deleted != 0 {
				t.Errorf("keep.txt stat = +%d -%d, want the pending line counted", f.added, f.deleted)
			}
		case "src/mod file.go":
			if f.added != 2 {
				t.Errorf("a path with a space lost its stat: +%d", f.added)
			}
		}
	}
}

func TestLoadChangesNeedsABase(t *testing.T) {
	dir := testRepo(t)
	for _, args := range [][]string{{"branch", "-m", "main", "trunk"}} {
		if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	if _, err := loadChanges(context.Background()); err == nil || !strings.Contains(err.Error(), "no base branch") {
		t.Errorf("err = %v, want no base branch", err)
	}
}

func TestRenderDiffPlainGit(t *testing.T) {
	dir := testRepo(t)
	ch, err := loadChanges(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]changedFile{}
	for _, f := range ch.files {
		byPath[f.path] = f
	}
	for path, want := range map[string]string{
		"keep.txt":        "+three",
		"notes/PLAN.md":   "+# plan", // untracked: diffed against /dev/null, exit status 1 is not an error
		"old.txt":         "-gone soon",
		"src/mod file.go": "+var x = 1",
	} {
		out, err := renderDiff(context.Background(), dir, ch.mergeBase, byPath[path], 80, false, "", nil)
		if err != nil || !strings.Contains(stripANSI(out), want) {
			t.Errorf("%s: err = %v, diff lacks %q:\n%s", path, err, want, stripANSI(out))
		}
	}
}

func TestRenderDiffCancelled(t *testing.T) {
	dir := testRepo(t)
	ch, _ := loadChanges(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	msg := renderPreviewCmd(ctx, dir, ch.mergeBase, ch.files[0], "k", 80, diffSingle, "")()
	if pm, ok := msg.(previewMsg); !ok || !pm.cancelled {
		t.Errorf("msg = %+v, want a cancelled render", msg)
	}
}

func TestRepoInfo(t *testing.T) {
	dir := testRepo(t)
	ri := loadRepoInfo()
	if ri.Top != dir || ri.Branch != "feature" || ri.Upstream != "" {
		t.Errorf("repo info = %+v", ri)
	}
	if s := ri.String(); !strings.HasSuffix(s, "  feature") {
		t.Errorf("String() = %q", s)
	}
}
