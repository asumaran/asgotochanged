package main

// What draws a diff: delta or hunk, or git's own colors when neither is
// around. The panel offers both renderers, the choice is remembered, and both get
// the same patch.
//
// This file is the same in every tool of the family that renders diffs.

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const (
	toolDelta = "delta" // also plain git, when delta is not installed
	toolHunk  = "hunk"
)

// diffTool is how the diff is made: what renders it (delta or hunk, or plain
// git when bin is empty) and whether git leaves the changes in whitespace out
// of the patch (its -w, what GitHub's "Hide whitespace" does).
type diffTool struct {
	name, bin string
	ignoreWS  bool
}

// toolBin resolves a renderer for the tool called name. <TOOL>_<RENDERER>
// (ASGITLOG_HUNK, ASGOTOCHANGED_DELTA) replaces the binary, and "none" turns
// the renderer off: the pty drivers do, so their frames do not depend on its
// looks.
func toolBin(name, renderer string) string {
	switch b := os.Getenv(strings.ToUpper(name + "_" + renderer)); b {
	case "":
	case "none":
		return ""
	default:
		return b
	}
	if p, err := exec.LookPath(renderer); err == nil {
		return p
	}
	return ""
}

// pickTool is the renderer for a preference: hunk when it is the one asked
// for and it is installed, else delta, whose bin may be empty.
func pickTool(pref, deltaBin, hunkBin string, ignoreWS bool) diffTool {
	if pref == toolHunk && hunkBin != "" {
		return diffTool{toolHunk, hunkBin, ignoreWS}
	}
	return diffTool{toolDelta, deltaBin, ignoreWS}
}

// plain reports that git's own colors are the render.
func (t diffTool) plain() bool { return t.bin == "" }

// colorArg is the color flag git gets: a renderer wants the bare patch.
func (t diffTool) colorArg() string {
	if t.plain() {
		return "--color=always"
	}
	return "--no-color"
}

// renderPatch draws patch, what git printed with tool.colorArg(), at width.
// delta ignores COLUMNS and falls back to 80 columns when stdout is not a
// tty, so the width is always explicit. early gets hunk's first frame, the
// diff before its syntax highlighting.
func renderPatch(ctx context.Context, tool diffTool, patch []byte, width int, sbs bool, early func(string)) (string, error) {
	if len(bytes.TrimSpace(patch)) == 0 {
		return "", nil
	}
	if !bytes.HasSuffix(patch, []byte("\n")) {
		patch = append(append([]byte{}, patch...), '\n')
	}
	switch {
	case tool.plain():
		return strings.ReplaceAll(strings.Trim(string(patch), "\n"), "\t", "    "), nil
	case tool.name == toolHunk:
		return renderHunk(ctx, tool.bin, patch, width, sbs, early)
	}
	args := []string{"--width=" + strconv.Itoa(width), "--paging=never"}
	if sbs {
		args = append(args, "--side-by-side")
	}
	delta := exec.CommandContext(ctx, tool.bin, args...)
	delta.Stdin = bytes.NewReader(patch)
	return limitedOutput(delta, false)
}

// diffPrefs is what both diff tools let the panel change about the diffs.
// Each tool keeps the values where it always did and persists them itself;
// the options, what each value means and what is said about a change are
// here, so the two cannot drift.
type diffPrefs struct {
	tool     string // toolDelta | toolHunk
	mode     string // diffAuto | diffSBS | diffSingle
	ignoreWS bool
}

var (
	rendererValues = []string{toolDelta, toolHunk}
	diffModeValues = []string{diffAuto, diffSBS, diffSingle}
)

// options lists them for the panel: the renderer has no key of its own, the
// mode and the whitespace keep theirs.
func (p diffPrefs) options() []option {
	at := func(values []string, v string) int {
		for i, x := range values {
			if x == v {
				return i
			}
		}
		return 0
	}
	ws := 0
	if p.ignoreWS {
		ws = 1
	}
	return []option{
		{id: "renderer", label: "Diff renderer", values: rendererValues, cur: at(rendererValues, p.tool)},
		{id: "diff", label: "Diff mode", values: []string{"auto", "side-by-side", "single column"}, cur: at(diffModeValues, p.mode), key: "^t"},
		{id: "whitespace", label: "Whitespace", values: []string{"show", "ignore"}, cur: ws, key: "^s"},
	}
}

// set applies value v of the option id and returns what to say about it.
// changed is false when id is not one of these options or when the renderer
// asked for is not installed (the flash says which). width is the diff
// area's, for the label of the auto mode.
func (p *diffPrefs) set(id string, v int, deltaBin, hunkBin string, width int) (flash string, changed bool) {
	switch id {
	case "renderer":
		want := rendererValues[max(0, min(v, len(rendererValues)-1))]
		if want == toolHunk && hunkBin == "" || want == toolDelta && deltaBin == "" {
			return want + " not found", false
		}
		p.tool = want
		return "diffs by " + want, true
	case "diff":
		p.mode = diffModeValues[max(0, min(v, len(diffModeValues)-1))]
		if pickTool(p.tool, deltaBin, hunkBin, p.ignoreWS).plain() {
			return "no renderer found: plain git colors", true
		}
		return "diff: " + diffLabel(p.mode, width), true
	case "whitespace":
		p.ignoreWS = v == 1
		if p.ignoreWS {
			return "whitespace: ignored", true
		}
		return "whitespace: shown", true
	}
	return "", false
}
