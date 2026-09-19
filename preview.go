package main

// Diff preview for the right-hand column: the file's diff since the merge
// base, rendered by hunk like asgitlog does (split or unified, by the diff
// mode and the width; see hunk.go) or, without hunk, by git's own colors.
// Rendering runs as a tea.Cmd under a context the model cancels when the
// selection moves on: a hunk render takes a few hundred milliseconds and
// walking the list would pile them up. hunk paints first and highlights
// later, so a render reports a partial frame and then the final one. Results
// are cached per (path, width, mode, mtime) for the popup's lifetime, so an
// edited file re-renders on its own.

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const (
	diffAuto   = "auto" // side by side when the preview is wide enough
	diffSBS    = "sbs"
	diffSingle = "single"

	// autoSBSMinW is the preview width from which the auto mode goes side by
	// side (the threshold asgitlog uses).
	autoSBSMinW = 120

	maxDiffBytes = 2 << 20
)

// effectiveDiff resolves auto by the preview width.
func effectiveDiff(mode string, width int) string {
	if mode != diffAuto {
		return mode
	}
	if width >= autoSBSMinW {
		return diffSBS
	}
	return diffSingle
}

func diffLabel(mode string, width int) string {
	label := "side-by-side"
	if effectiveDiff(mode, width) == diffSingle {
		label = "single column"
	}
	if mode == diffAuto {
		return "auto: " + label
	}
	return label
}

func nextDiffMode(mode string) string {
	switch mode {
	case diffAuto:
		return diffSBS
	case diffSBS:
		return diffSingle
	}
	return diffAuto
}

// hunkPath resolves hunk. GOTOCHANGED_HUNK replaces it, and "none" turns it
// off (the pty driver does, so its frames do not depend on hunk's looks).
func hunkPath() string {
	switch b := os.Getenv("GOTOCHANGED_HUNK"); b {
	case "":
	case "none":
		return ""
	default:
		return b
	}
	if p, err := exec.LookPath("hunk"); err == nil {
		return p
	}
	return ""
}

type previewMsg struct {
	key     string
	content string
	// partial marks hunk's first frame: the diff is there, the syntax
	// highlighting is not. It is shown and never cached.
	partial bool
	// cancelled marks a render whose context was cancelled because the
	// selection moved on; it carries nothing worth showing.
	cancelled bool
	// next waits for what follows a partial render: the final one.
	next tea.Cmd
}

// previewKey identifies a render. mode is the effective diff mode, so auto
// and an explicit mode share their renders; the mtime makes a render of an
// edited file unreachable.
func previewKey(top string, f changedFile, width int, mode string) string {
	var mtime int64
	if st, err := os.Stat(filepath.Join(top, f.path)); err == nil {
		mtime = st.ModTime().UnixNano()
	}
	return f.status + "|" + f.path + "|" + strconv.Itoa(width) + "|" + mode + "|" + strconv.FormatInt(mtime, 10)
}

func renderPreviewCmd(ctx context.Context, top, mergeBase string, f changedFile, key string, width int, mode, hunkBin string) tea.Cmd {
	// At most a partial and a final message: the render never blocks on a
	// program that went away.
	msgs := make(chan previewMsg, 2)
	next := func() tea.Msg { return <-msgs }
	rendered := func(body string, partial bool) previewMsg {
		if strings.TrimSpace(body) == "" {
			body = stDim.Render("(no textual changes)")
		}
		return previewMsg{key: key, content: body, partial: partial}
	}
	run := func() previewMsg {
		body, err := renderDiff(ctx, top, mergeBase, f, width, mode == diffSBS, hunkBin, func(body string) {
			msg := rendered(body, true)
			msg.next = next
			msgs <- msg
		})
		if ctx.Err() != nil {
			return previewMsg{key: key, cancelled: true}
		}
		if err != nil {
			return rendered(stError.Render(truncate(err.Error(), width)), false)
		}
		return rendered(body, false)
	}
	return func() tea.Msg {
		go func() { msgs <- run() }()
		return next()
	}
}

// renderDiff renders the file's patch with hunk, or returns git's colored
// diff when there is no hunk. early gets hunk's first frame.
func renderDiff(ctx context.Context, top, mergeBase string, f changedFile, width int, sbs bool, hunkBin string, early func(string)) (string, error) {
	git := exec.CommandContext(ctx, "git", diffArgs(top, mergeBase, f, hunkBin == "")...)
	// `git diff --no-index` exits 1 for "there are differences".
	out, err := limitedOutput(git, f.status == "?")
	if err != nil {
		return "", err
	}
	if hunkBin == "" {
		return strings.ReplaceAll(out, "\t", "    "), nil
	}
	return renderHunk(ctx, hunkBin, []byte(out+"\n"), width, sbs, early)
}

// limitedOutput runs cmd and returns at most maxDiffBytes of its stdout, cut
// at a line boundary with a note when the cap was hit. diffExit tolerates
// exit status 1, which `git diff --no-index` uses for "there are differences".
func limitedOutput(cmd *exec.Cmd, diffExit bool) (string, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	data, _ := io.ReadAll(io.LimitReader(stdout, maxDiffBytes+1))
	capped := len(data) > maxDiffBytes
	if capped {
		_ = cmd.Process.Kill()
		data = data[:maxDiffBytes]
		if i := bytes.LastIndexByte(data, '\n'); i >= 0 {
			data = data[:i]
		}
	}
	err = cmd.Wait()
	out := strings.Trim(string(data), "\n")
	if capped {
		return out + "\n\n" + stDim.Render("(diff truncated at "+strconv.Itoa(maxDiffBytes>>20)+" MiB)"), nil
	}
	if ee, ok := err.(*exec.ExitError); ok && diffExit && ee.ExitCode() == 1 {
		return out, nil
	}
	if err != nil && out == "" {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", &renderError{strings.SplitN(msg, "\n", 2)[0]}
		}
		return "", err
	}
	return out, nil
}

type renderError struct{ msg string }

func (e *renderError) Error() string { return e.msg }
