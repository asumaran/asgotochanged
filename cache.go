package main

// The rendered diffs, kept on disk between runs. hunk takes its time (it
// paints the diff first and the syntax highlighting a few hundred
// milliseconds later), and the popup is opened over and over on the same
// branch. A render is addressed by the patch it was made from, so an edited
// file simply misses: nothing here can go stale. Same scheme as asgitlog,
// which keys on the commit because a commit never changes.

import (
	"bytes"
	"compress/gzip"
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
	cacheFormat   = "1"      // bump when what is stored changes
	cacheMaxBytes = 64 << 20 // pruned down to 3/4 of this at startup
)

// diskCache stores gzipped renders under dir, one file per key. A nil cache
// (tests, GOTOCHANGED_NO_CACHE) stores nothing.
type diskCache struct {
	dir string

	once sync.Once
	hunk string // fingerprint of hunk's binary and configuration
}

// renderCache is opened by main(); nil keeps nothing.
var renderCache *diskCache

// cacheDir is ${XDG_CACHE_HOME:-~/.cache}/gotochanged/renders.
func cacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "gotochanged", "renders")
}

func openDiskCache() *diskCache {
	dir := cacheDir()
	if dir == "" || os.Getenv("GOTOCHANGED_NO_CACHE") != "" {
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

// fingerprint covers what changes hunk's output besides the patch: its binary
// and its configuration files (user-wide, saved view preferences, per repo).
func (c *diskCache) fingerprint(hunkBin, top string) string {
	c.once.Do(func() {
		base := os.Getenv("XDG_CONFIG_HOME")
		if home, err := os.UserHomeDir(); base == "" && err == nil {
			base = filepath.Join(home, ".config")
		}
		c.hunk = strings.Join([]string{
			fileMark(hunkBin),
			fileMark(filepath.Join(base, "hunk", "config.toml")),
			fileMark(filepath.Join(base, "hunk", "state.json")),
			fileMark(filepath.Join(top, ".hunk", "config.toml")),
		}, "\x00")
	})
	return c.hunk
}

func (c *diskCache) path(hunkBin, top string, patch []byte, width int, mode string) string {
	h := sha256.New()
	h.Write([]byte(strings.Join([]string{cacheFormat, c.fingerprint(hunkBin, top), strconv.Itoa(width), mode}, "\x00")))
	h.Write([]byte{0})
	h.Write(patch)
	name := hex.EncodeToString(h.Sum(nil))
	return filepath.Join(c.dir, name[:2], name[2:]+".gz")
}

// get returns a stored render. A hit is touched, so pruning drops what has
// not been looked at for the longest.
func (c *diskCache) get(hunkBin, top string, patch []byte, width int, mode string) (string, bool) {
	if c == nil {
		return "", false
	}
	p := c.path(hunkBin, top, patch, width, mode)
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

// put stores a render. Failures only cost the next run a render.
func (c *diskCache) put(hunkBin, top string, patch []byte, width int, mode, diff string) {
	if c == nil {
		return
	}
	p := c.path(hunkBin, top, patch, width, mode)
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
