package main

// Diff preview for the right-hand column: the file's diff since the merge
// base, rendered by hunk like asgitlog does (split or unified, by the diff
// mode and the width; see hunk.go) or, without hunk, by git's own colors.
// Rendering runs as a tea.Cmd under a context the model cancels when the
// selection moves on: a hunk render takes a few hundred milliseconds and
// walking the list would pile them up. hunk paints first and highlights
// later, so a render reports a partial frame and then the final one; to keep
// that repaint off the screen the model renders the rows around the cursor
// ahead of time, and the finished renders are kept on disk between runs (see
// cache.go). In memory they are cached per (path, width, mode, mtime), so an
// edited file re-renders on its own.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const (
	maxDiffBytes = 2 << 20
)

// fileDiff is the effective mode for one file. In auto, a file that was only
// added or only deleted goes single column whatever the width: side by side
// would leave one half empty and cut the other at the middle.
func fileDiff(mode string, width int, f changedFile) string {
	if mode == diffAuto && (f.status == "A" || f.status == "D" || f.status == "?") {
		return diffSingle
	}
	return effectiveDiff(mode, width)
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
func previewKey(top string, f changedFile, width int, mode string, tool diffTool) string {
	var mtime int64
	if st, err := os.Stat(filepath.Join(top, f.path)); err == nil {
		mtime = st.ModTime().UnixNano()
	}
	if tool.ignoreWS {
		mode += "-w"
	}
	return f.status + "|" + f.path + "|" + strconv.Itoa(width) + "|" + tool.name + "|" + mode + "|" + strconv.FormatInt(mtime, 10)
}

func renderPreviewCmd(ctx context.Context, top, mergeBase string, f changedFile, key string, width int, mode string, tool diffTool) tea.Cmd {
	// At most a partial and a final message: the render never blocks on a
	// program that went away.
	msgs := make(chan previewMsg, 2)
	next := func() tea.Msg { return <-msgs }
	rendered := func(body string, partial bool) previewMsg {
		if strings.TrimSpace(body) == "" {
			body = stDim.Render("(no textual changes)")
			if tool.ignoreWS {
				body = stDim.Render("(only whitespace changes)")
			}
		}
		return previewMsg{key: key, content: body, partial: partial}
	}
	run := func() previewMsg {
		body, err := renderDiff(ctx, top, mergeBase, f, width, mode == diffSBS, tool, func(body string) {
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

// renderDiff draws the file's patch with tool (see renderPatch); early gets
// hunk's first frame.
func renderDiff(ctx context.Context, top, mergeBase string, f changedFile, width int, sbs bool, tool diffTool, early func(string)) (string, error) {
	git := exec.CommandContext(ctx, "git", diffArgs(top, mergeBase, f, tool.plain(), tool.ignoreWS)...)
	// `git diff --no-index` exits 1 for "there are differences".
	out, err := limitedOutput(git, f.status == "?")
	if err != nil {
		return "", err
	}
	// With -w a file that only changed in whitespace has no hunks left; some
	// git versions still print its header, which is nothing to show.
	if tool.ignoreWS && !strings.Contains(out, "@@ -") && !strings.Contains(out, "Binary files") {
		return "", nil
	}
	patch := []byte(out + "\n")
	if tool.plain() { // as fast as reading it back
		return renderPatch(ctx, tool, patch, width, sbs, early)
	}
	// The render is addressed by the patch itself (see rendercache.go): a hit
	// is the finished, highlighted frame at once, with no partial frame before
	// it, and an edited file simply misses.
	id, mode := patchID(patch), diffSingle
	if sbs {
		mode = diffSBS
	}
	if diff, ok := renderCache.get(tool, id, width, mode); ok {
		return diff, nil
	}
	diff, err := renderPatch(ctx, tool, patch, width, sbs, early)
	if err == nil && ctx.Err() == nil {
		renderCache.put(tool, id, width, mode, diff)
	}
	return diff, err
}
