package main

// The rendered diffs, kept on disk between runs. delta and hunk take their
// time (hunk paints the diff first and the syntax highlighting a few hundred
// milliseconds later), and a popup is opened over and over on the same
// things. A render is addressed by an id the caller gives it and that can
// never go stale: a commit's hash, or a hash of the patch itself for what is
// still being edited. Whatever else changes the output (the tool, its binary,
// its configuration, the width, the mode) is part of the address too.
//
// This file is the same in every tool of the family that renders diffs.

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	cacheFormat   = "2"       // bump when what is stored changes
	cacheMaxBytes = 128 << 20 // pruned down to 3/4 of this at startup
)

// diskCache stores gzipped renders under dir, one file per key. A nil cache
// (tests, <TOOL>_NO_CACHE) stores nothing.
type diskCache struct {
	dir string

	once  sync.Once
	tools map[string]string // tool name → fingerprint of its binary and configuration
}

// renderCache is opened by main(); nil keeps nothing.
var renderCache *diskCache

// cacheDir is ${XDG_CACHE_HOME:-~/.cache}/<tool>/renders.
func cacheDir(tool string) string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, tool, "renders")
}

// openDiskCache is the cache of the tool called name; <TOOL>_NO_CACHE turns it
// off.
func openDiskCache(name string) *diskCache {
	dir := cacheDir(name)
	if dir == "" || os.Getenv(strings.ToUpper(name)+"_NO_CACHE") != "" {
		return nil
	}
	return &diskCache{dir: dir}
}

// fileMark identifies a file's current content cheaply.
func fileMark(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return path + ":-"
	}
	return path + ":" + strconv.FormatInt(st.Size(), 10) + ":" + strconv.FormatInt(st.ModTime().UnixNano(), 10)
}

// fingerprint covers what changes a tool's output besides the patch: its
// binary and its configuration. delta is configured through git (any scope,
// includes too), hunk through its own files.
func (c *diskCache) fingerprint(tool diffTool) string {
	c.once.Do(func() {
		conf, _ := runGit(context.Background(), "config", "--list")
		c.tools = map[string]string{
			toolDelta: conf + "\x00" + os.Getenv("DELTA_FEATURES") + "\x00" + os.Getenv("BAT_THEME"),
		}
		base := os.Getenv("XDG_CONFIG_HOME")
		if home, err := os.UserHomeDir(); base == "" && err == nil {
			base = filepath.Join(home, ".config")
		}
		top, _ := runGit(context.Background(), "rev-parse", "--show-toplevel")
		c.tools[toolHunk] = strings.Join([]string{
			fileMark(filepath.Join(base, "hunk", "config.toml")),
			fileMark(filepath.Join(base, "hunk", "state.json")), // saved view preferences
			fileMark(filepath.Join(strings.TrimSpace(top), ".hunk", "config.toml")),
		}, "\x00")
	})
	return tool.name + "\x00" + fileMark(tool.bin) + "\x00" + c.tools[tool.name]
}

// patchID is the id of a render made from patch: an edited file gives another
// patch, so its old renders are simply never asked for again.
func patchID(patch []byte) string {
	sum := sha256.Sum256(patch)
	return hex.EncodeToString(sum[:])
}

func (c *diskCache) path(tool diffTool, id string, width int, mode string) string {
	if tool.ignoreWS {
		mode += "-w"
	}
	sum := sha256.Sum256([]byte(strings.Join([]string{
		cacheFormat, c.fingerprint(tool), id, strconv.Itoa(width), mode,
	}, "\x00")))
	name := hex.EncodeToString(sum[:])
	return filepath.Join(c.dir, name[:2], name[2:]+".gz")
}

// get returns a stored render. A hit is touched, so pruning drops what has
// not been looked at for the longest.
func (c *diskCache) get(tool diffTool, id string, width int, mode string) (string, bool) {
	if c == nil || id == "" || tool.plain() { // plain git is as fast as reading it back
		return "", false
	}
	p := c.path(tool, id, width, mode)
	f, err := os.Open(p)
	if err != nil {
		return "", false
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return "", false
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		return "", false
	}
	now := time.Now()
	_ = os.Chtimes(p, now, now)
	return string(out), true
}

// put stores a render; one without an id (asgitlog's working tree row) is
// never stored. Failures only cost the next run a render.
func (c *diskCache) put(tool diffTool, id string, width int, mode, diff string) {
	if c == nil || id == "" || tool.plain() {
		return
	}
	p := c.path(tool, id, width, mode)
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil {
		return
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	_, _ = zw.Write([]byte(diff))
	if zw.Close() != nil {
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return
	}
	_, err = tmp.Write(buf.Bytes())
	if cerr := tmp.Close(); err != nil || cerr != nil || os.Rename(tmp.Name(), p) != nil {
		_ = os.Remove(tmp.Name())
	}
}

// prune keeps the cache under max bytes, dropping the renders that were used
// the longest ago until 3/4 of it is left.
func (c *diskCache) prune(max int64) {
	if c == nil {
		return
	}
	type entry struct {
		path string
		size int64
		used time.Time
	}
	var entries []entry
	var total int64
	_ = filepath.WalkDir(c.dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			entries = append(entries, entry{p, info.Size(), info.ModTime()})
			total += info.Size()
		}
		return nil
	})
	if total <= max {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].used.Before(entries[j].used) })
	for _, e := range entries {
		if total <= max/4*3 {
			break
		}
		if os.Remove(e.path) == nil {
			total -= e.size
		}
	}
}
