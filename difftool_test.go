package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPickTool(t *testing.T) {
	if got := pickTool(toolHunk, "/bin/delta", "/bin/hunk", true); got != (diffTool{toolHunk, "/bin/hunk", true}) {
		t.Errorf("hunk asked for and installed: %+v", got)
	}
	if got := pickTool(toolHunk, "/bin/delta", "", false); got != (diffTool{toolDelta, "/bin/delta", false}) {
		t.Errorf("hunk asked for but missing falls back to delta: %+v", got)
	}
	if got := pickTool("", "", "/bin/hunk", false); !got.plain() || got.colorArg() != "--color=always" {
		t.Errorf("no delta: git's own colors, %+v", got)
	}
}

func TestToolBinFollowsTheToolsVariable(t *testing.T) {
	t.Setenv("ASTOOL_HUNK", "none")
	if got := toolBin("astool", "hunk"); got != "" {
		t.Errorf("none turns the renderer off: %q", got)
	}
	t.Setenv("ASTOOL_HUNK", "/opt/hunk")
	if got := toolBin("astool", "hunk"); got != "/opt/hunk" {
		t.Errorf("the variable replaces the binary: %q", got)
	}
}

func TestRenderPatchFeedsDeltaThePatchAndTheWidth(t *testing.T) {
	stub := filepath.Join(t.TempDir(), "delta")
	script := "#!/bin/sh\necho \"args: $*\"\ncat\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	patch := []byte("diff --git a/x b/x\n@@ -1 +1 @@\n-a\n+b")
	out, err := renderPatch(context.Background(), diffTool{name: toolDelta, bin: stub}, patch, 97, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "args: --width=97 --paging=never --side-by-side\n") || !strings.HasSuffix(out, "+b") {
		t.Errorf("delta got %q", out)
	}
	plain, _ := renderPatch(context.Background(), diffTool{name: toolDelta}, []byte("\tindented\n"), 80, false, nil)
	if plain != "    indented" {
		t.Errorf("plain render = %q, want the tabs expanded", plain)
	}
	if empty, _ := renderPatch(context.Background(), diffTool{name: toolDelta, bin: stub}, []byte("\n"), 80, false, nil); empty != "" {
		t.Errorf("an empty patch renders nothing: %q", empty)
	}
}
