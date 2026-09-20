// asgotochanged: a herdr plugin popup that lists the files the current branch
// changed against its base (what a PR would ship, plus what is still pending
// and untracked), shows the diff of the one under the cursor rendered by hunk,
// and opens it in the editor. It also runs as a plain command in any git
// checkout. It replaces fm, a zsh + fzf function.
//
// It only reads the repository: it never stages, commits or checks anything
// out. What it writes is its own: the chosen diff mode and a cache of renders.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// version is the release tag; overridden at build time via
// -ldflags "-X main.version=vX.Y.Z" (see scripts/release.sh and CI).
var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print the embedded version")
	dump := flag.Bool("dump", false, "print the changed files (no TUI)")
	query := flag.String("query", "", "initial filter; with -dump, print the matches and their scores")
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "usage: asgotochanged [flags] [query]")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}
	if *query == "" && flag.NArg() > 0 {
		*query = flag.Arg(0)
	}

	enterPaneCwd()
	if !insideWorkTree() {
		fatal("not inside a git work tree: " + cwd())
	}

	start := time.Now()
	repo := loadRepoInfo()
	ch, err := loadChanges(context.Background())

	if *dump {
		if err != nil {
			fmt.Fprintln(os.Stderr, "asgotochanged:", err)
			os.Exit(1)
		}
		runDump(repo, ch, *query, time.Since(start))
		return
	}

	loadErr := ""
	if err != nil {
		loadErr = err.Error()
	}
	renderCache = openDiskCache()
	go renderCache.prune(cacheMaxBytes)

	// Alt screen and mouse mode are declared per frame by View().
	m := newModel(repo, ch, loadErr, loadDiffMode(), hunkPath(), *query)
	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// fatal reports why there is nothing to show. In a popup the pane closes the
// instant the process exits, so the message is held until a key is pressed.
func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "asgotochanged:", msg)
	if os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != "" {
		fmt.Fprint(os.Stderr, "press enter to close…")
		_, _ = fmt.Scanln()
	}
	os.Exit(1)
}

func cwd() string {
	dir, _ := os.Getwd()
	return homeRel(dir)
}

// enterPaneCwd moves to the directory of the pane that was focused when the
// popup opened. herdr starts plugin panes in the plugin's own directory (the
// manifest's "./asgotochanged" is resolved against it, so the pane cannot simply
// be opened with another cwd) and describes the invocation, focused pane
// included, in HERDR_PLUGIN_CONTEXT_JSON.
func enterPaneCwd() {
	if os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") == "" {
		return
	}
	var ctx struct {
		FocusedPaneCwd string `json:"focused_pane_cwd"`
		WorkspaceCwd   string `json:"workspace_cwd"`
	}
	if json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &ctx) != nil {
		return
	}
	for _, dir := range []string{ctx.FocusedPaneCwd, ctx.WorkspaceCwd} {
		if dir != "" && os.Chdir(dir) == nil {
			return
		}
	}
}

// ---- preferences ----

func loadDiffMode() string {
	data, _ := os.ReadFile(filepath.Join(stateDir(), "diff"))
	if d := strings.TrimSpace(string(data)); d == diffSBS || d == diffSingle {
		return d
	}
	return diffAuto
}

func saveDiffMode(mode string) {
	_ = os.MkdirAll(stateDir(), 0o755)
	_ = os.WriteFile(filepath.Join(stateDir(), "diff"), []byte(mode+"\n"), 0o644)
}

func loadIgnoreWS() bool {
	data, _ := os.ReadFile(filepath.Join(stateDir(), "whitespace"))
	return strings.TrimSpace(string(data)) == "ignore"
}

func saveIgnoreWS(ignore bool) {
	value := "show"
	if ignore {
		value = "ignore"
	}
	_ = os.MkdirAll(stateDir(), 0o755)
	_ = os.WriteFile(filepath.Join(stateDir(), "whitespace"), []byte(value+"\n"), 0o644)
}

func wsLabel(ignore bool) string {
	if ignore {
		return "ignored"
	}
	return "shown"
}

// runDump prints what the popup would list, without a TTY. With -query it
// prints the matches and their scores instead.
func runDump(repo repoInfo, ch changes, query string, took time.Duration) {
	fmt.Printf("%s\nbase: %s (merge base %.12s), %d files, loaded in %s\n",
		repo, ch.base, ch.mergeBase, len(ch.files), took.Round(time.Millisecond))
	if query != "" {
		fmt.Printf("query %q:\n", query)
		for _, r := range filterFiles(ch.files, query) {
			fmt.Printf("  %5d  %s %s\n", r.score, r.f.status, r.f.path)
		}
		return
	}
	for _, f := range ch.files {
		stat := fmt.Sprintf("+%d -%d", f.added, f.deleted)
		switch {
		case f.binary:
			stat = "binary"
		case f.status == "?":
			stat = ""
		}
		fmt.Printf("%s  %-60s %s\n", f.status, f.path, stat)
	}
}

// stateDir is where asgotochanged keeps its runtime state.
func stateDir() string { return stateDirFor("asgotochanged") }
