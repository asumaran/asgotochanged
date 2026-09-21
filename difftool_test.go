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

func TestDiffPrefs(t *testing.T) {
	p := diffPrefs{tool: toolHunk, mode: diffAuto}
	opts := p.options()
	if len(opts) != 3 || opts[0].cur != 1 || opts[0].key != "" || opts[1].key != "^t" || opts[2].key != "^s" || opts[2].cur != 0 {
		t.Fatalf("options = %+v", opts)
	}
	if flash, ok := p.set("renderer", 0, "", "/bin/hunk", 100); ok || flash != "delta not found" || p.tool != toolHunk {
		t.Errorf("delta asked for and missing: %q %v %q", flash, ok, p.tool)
	}
	if flash, ok := p.set("renderer", 1, "/bin/delta", "", 100); ok || flash != "hunk not found" {
		t.Errorf("hunk asked for and missing: %q %v", flash, ok)
	}
	if flash, ok := p.set("renderer", 0, "/bin/delta", "", 100); !ok || flash != "diffs by delta" || p.tool != toolDelta {
		t.Errorf("delta can be chosen without hunk installed: %q %v %q", flash, ok, p.tool)
	}
	if flash, ok := p.set("diff", 1, "/bin/delta", "", 100); !ok || flash != "diff: side-by-side" || p.mode != diffSBS {
		t.Errorf("diff mode: %q %v %q", flash, ok, p.mode)
	}
	// The warning is about the renderer in use, not about delta.
	q := diffPrefs{tool: toolHunk, mode: diffAuto}
	if flash, _ := q.set("diff", 2, "", "/bin/hunk", 100); flash != "diff: single column" {
		t.Errorf("hunk renders fine without delta: %q", flash)
	}
	if flash, _ := q.set("diff", 0, "", "", 100); flash != "no renderer found: plain git colors" {
		t.Errorf("neither renderer: %q", flash)
	}
	if flash, ok := q.set("whitespace", 1, "", "", 100); !ok || flash != "whitespace: ignored" || !q.ignoreWS {
		t.Errorf("whitespace: %q %v", flash, ok)
	}
	if _, ok := q.set("layout", 1, "", "", 100); ok {
		t.Errorf("not a diff option")
	}
}
