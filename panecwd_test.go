package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPaneDirs(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	ws, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", `{"focused_pane_cwd":"`+gone+`","workspace_cwd":"`+ws+`"}`)

	// a plain run: nothing from herdr, the working directory counts
	t.Setenv("HERDR_PLUGIN_ENTRYPOINT_ID", "")
	wd, _ := os.Getwd()
	if dirs := paneDirs(); dirs != nil || paneCwd() != wd {
		t.Errorf("plain run: dirs %v, cwd %q, want none and %q", dirs, paneCwd(), wd)
	}

	t.Setenv("HERDR_PLUGIN_ENTRYPOINT_ID", "open")
	if dirs := paneDirs(); len(dirs) != 2 || dirs[0] != gone || paneCwd() != gone {
		t.Errorf("plugin pane: dirs %v, cwd %q", dirs, paneCwd())
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	enterPaneCwd()
	if got, _ := os.Getwd(); got != ws {
		t.Errorf("enterPaneCwd went to %q, want the workspace (the pane's dir is gone)", got)
	}
	t.Setenv("HERDR_PLUGIN_CONTEXT_JSON", "not json")
	if paneDirs() != nil || paneCwd() != "" {
		t.Errorf("a broken context gives no directory: %v %q", paneDirs(), paneCwd())
	}
}
