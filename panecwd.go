package main

// Which directory a popup was opened from.
//
// This file is the same in every tool of the family that needs it.

import (
	"encoding/json"
	"os"
)

// paneDirs are the directories herdr says the popup was opened from, the
// focused pane's first and its workspace's after it. herdr starts a plugin
// pane in the plugin's own directory and hands these over, overlays included,
// in HERDR_PLUGIN_CONTEXT_JSON. Outside a plugin pane there are none.
func paneDirs() []string {
	if os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") == "" {
		return nil
	}
	var ctx struct {
		FocusedPaneCwd string `json:"focused_pane_cwd"`
		WorkspaceCwd   string `json:"workspace_cwd"`
	}
	if json.Unmarshal([]byte(os.Getenv("HERDR_PLUGIN_CONTEXT_JSON")), &ctx) != nil {
		return nil
	}
	var dirs []string
	for _, dir := range []string{ctx.FocusedPaneCwd, ctx.WorkspaceCwd} {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// paneCwd is the directory the tool is about: the first of paneDirs in a
// plugin pane ("" when herdr gave none), the working directory in a plain run.
func paneCwd() string {
	if os.Getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != "" {
		if dirs := paneDirs(); len(dirs) > 0 {
			return dirs[0]
		}
		return ""
	}
	dir, _ := os.Getwd()
	return dir
}

// enterPaneCwd moves into the first of paneDirs that still exists, for the
// tools that work on the current directory. A plain run stays where it is.
func enterPaneCwd() {
	for _, dir := range paneDirs() {
		if os.Chdir(dir) == nil {
			return
		}
	}
}
