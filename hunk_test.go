package main

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHunkRows(t *testing.T) {
	patch := []byte("diff --git a/x b/x\n" + strings.Repeat("+line\n", 30) + "diff --git a/y b/y\n+line\n")
	// A row per line, the chrome of the second file (the first "diff --git"
	// has no newline before it) and 12 of slack.
	if rows, capped := hunkRows(patch); rows != 33+6+12 || capped {
		t.Errorf("rows = %d capped=%v", rows, capped)
	}
	big := bytes.Repeat([]byte("+line\n"), hunkMaxRows+1)
	if rows, capped := hunkRows(big); rows != hunkMaxRows || !capped {
		t.Errorf("big patch: rows = %d capped=%v", rows, capped)
	}
}

const hunkTestPatch = `diff --git a/total.go b/total.go
index 1111111..2222222 100644
--- a/total.go
+++ b/total.go
@@ -1,3 +1,3 @@
 package cart
 
-var total = net
+var total = net + tax
`

// TestRenderHunk runs the real hunk: nothing else says what it paints.
func TestRenderHunk(t *testing.T) {
	hunkBin, err := exec.LookPath("hunk")
	if err != nil {
		t.Skip("hunk not installed")
	}
	if out, err := renderHunk(context.Background(), hunkBin, []byte(" \n"), 90, true, nil); out != "" || err != nil {
		t.Errorf("an empty patch renders nothing: %q, %v", out, err)
	}
	for _, sbs := range []bool{true, false} {
		out, err := renderHunk(context.Background(), hunkBin, []byte(hunkTestPatch), 90, sbs, nil)
		if err != nil {
			t.Fatal(err)
		}
		if plain := ansi.Strip(out); !strings.Contains(plain, "total.go") || !strings.Contains(plain, "net + tax") {
			t.Errorf("sbs=%v: diff content missing:\n%s", sbs, plain)
		}
		lines := strings.Split(out, "\n")
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > 90 {
				t.Errorf("sbs=%v: line is %d cells, over the pty's 90", sbs, w)
			}
		}
		if strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
			t.Errorf("sbs=%v: the blank rows of the tall screen should be cut", sbs)
		}
	}
	// The first frame is handed over before the syntax highlighting is in:
	// the same text, so the final render replaces it without anything moving.
	var first string
	out, err := renderHunk(context.Background(), hunkBin, []byte(hunkTestPatch), 90, true, func(s string) { first = s })
	if err != nil || first == "" || ansi.Strip(first) != ansi.Strip(out) {
		t.Errorf("early frame: err=%v\n%s\n--- final:\n%s", err, ansi.Strip(first), ansi.Strip(out))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := renderHunk(ctx, hunkBin, []byte(hunkTestPatch), 90, true, nil); err == nil {
		t.Error("a cancelled render should fail")
	}
}
