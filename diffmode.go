package main

// How a diff is laid out and fetched: the modes ctrl+t walks, what the main
// section's bottom edge says about the diff, and running the command that
// prints it without letting a huge one in. maxDiffBytes is each tool's own.
//
// This file is the same in every tool of the family that shows diffs.

import (
	"bytes"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

const (
	diffAuto   = "auto" // side by side when the preview is wide enough
	diffSBS    = "sbs"
	diffSingle = "single"

	// autoSBSMinW is the preview width from which the auto mode goes side by
	// side (the threshold the dotfiles' delta-pager wrapper uses).
	autoSBSMinW = 120
)

// effectiveDiff resolves the auto mode for a preview of the given width.
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

// diffEdge is what the main section's bottom edge says about the diff: a mark
// while git's -w is on, and the scroll position.
func diffEdge(ignoreWS bool, pos string) string {
	if !ignoreWS {
		return pos
	}
	mark := stScope.Render("[-w]")
	if pos == "" {
		return mark
	}
	return mark + stDim.Render(" ─ ") + pos
}

// limitedOutput runs cmd and returns at most maxDiffBytes of its stdout, cut
// at a line boundary with a note when the cap was hit. diffExit tolerates
// exit status 1, which `git diff --no-index` uses for "there are differences".
// diffExit says an exit status of 1 means "there are differences", as it does
// for `git diff --no-index`.
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
			return "", &renderError{firstLine(msg)}
		}
		return "", err
	}
	return out, nil
}

type renderError struct{ msg string }

func (e *renderError) Error() string { return e.msg }
