package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testCache(t *testing.T) *diskCache {
	t.Helper()
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	c := openDiskCache("astool")
	if c == nil || !strings.HasSuffix(c.dir, filepath.Join("astool", "renders")) {
		t.Fatalf("cache = %+v", c)
	}
	return c
}

func TestDiskCacheRoundTrip(t *testing.T) {
	c := testCache(t)
	bin := filepath.Join(t.TempDir(), "delta")
	os.WriteFile(bin, []byte("v1"), 0o755)
	tool := diffTool{name: toolDelta, bin: bin}
	if _, ok := c.get(tool, "abc", 100, diffSBS); ok {
		t.Fatal("empty cache hit")
	}
	c.put(tool, "abc", 100, diffSBS, "\x1b[32mthe diff\x1b[m\nline 2")
	if got, ok := c.get(tool, "abc", 100, diffSBS); !ok || got != "\x1b[32mthe diff\x1b[m\nline 2" {
		t.Errorf("get = %q, %v", got, ok)
	}
	// Anything the render depends on is part of the key.
	for name, hit := range map[string]bool{
		"width": func() bool { _, ok := c.get(tool, "abc", 101, diffSBS); return ok }(),
		"mode":  func() bool { _, ok := c.get(tool, "abc", 100, diffSingle); return ok }(),
		"tool":  func() bool { _, ok := c.get(diffTool{name: toolHunk, bin: bin}, "abc", 100, diffSBS); return ok }(),
		"hash":  func() bool { _, ok := c.get(tool, "abd", 100, diffSBS); return ok }(),
	} {
		if hit {
			t.Errorf("a different %s must miss", name)
		}
	}
	// The working tree changes under the same (empty) hash: never stored.
	c.put(tool, "", 100, diffSBS, "wt")
	if _, ok := c.get(tool, "", 100, diffSBS); ok {
		t.Error("the working tree row must not be cached")
	}
	// A nil cache is a cache that is off.
	var off *diskCache
	off.put(tool, "abc", 100, diffSBS, "x")
	off.prune(1)
	if _, ok := off.get(tool, "abc", 100, diffSBS); ok {
		t.Error("nil cache hit")
	}
	t.Setenv("ASTOOL_NO_CACHE", "1")
	if openDiskCache("astool") != nil {
		t.Error("ASTOOL_NO_CACHE should turn the cache off")
	}
}

func TestDiskCacheFollowsTheTool(t *testing.T) {
	c := testCache(t)
	bin := filepath.Join(t.TempDir(), "hunk")
	os.WriteFile(bin, []byte("v1"), 0o755)
	tool := diffTool{name: toolHunk, bin: bin}
	c.put(tool, "abc", 100, diffSBS, "old looks")

	// A new binary (an upgrade) is a new fingerprint for the next run.
	os.WriteFile(bin, []byte("version 2"), 0o755)
	if _, ok := (&diskCache{dir: c.dir}).get(tool, "abc", 100, diffSBS); ok {
		t.Error("an upgraded tool must not be served the old renders")
	}
	// So is a change to its configuration.
	next := &diskCache{dir: c.dir}
	next.put(tool, "abc", 100, diffSBS, "new looks")
	conf := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "hunk", "config.toml")
	os.MkdirAll(filepath.Dir(conf), 0o755)
	os.WriteFile(conf, []byte(`theme = "x"`), 0o644)
	if _, ok := (&diskCache{dir: c.dir}).get(tool, "abc", 100, diffSBS); ok {
		t.Error("a changed configuration must miss")
	}
	if got, ok := next.get(tool, "abc", 100, diffSBS); !ok || got != "new looks" {
		t.Errorf("within a run the fingerprint is taken once: %q %v", got, ok)
	}
}

func TestDiskCachePrune(t *testing.T) {
	c := testCache(t)
	tool := diffTool{name: toolDelta, bin: "/nonexistent/delta"}
	body := strings.Repeat("x", 4000) // incompressible enough: sizes are what gzip leaves
	for i, hash := range []string{"old", "mid", "new"} {
		c.put(tool, hash, 100, diffSBS, body+hash)
		when := time.Now().Add(time.Duration(i-3) * time.Hour)
		os.Chtimes(c.path(tool, hash, 100, diffSBS), when, when)
	}
	st, _ := os.Stat(c.path(tool, "old", 100, diffSBS))
	c.prune(10 * st.Size()) // under the limit: nothing goes
	if _, ok := c.get(tool, "old", 100, diffSBS); !ok {
		t.Fatal("pruned under the limit")
	}
	// "old" was just read, which makes "mid" the least recently used.
	// Over it: down to 3/4 of the limit, which leaves room for one.
	c.prune(2 * st.Size())
	_, old := c.get(tool, "old", 100, diffSBS)
	_, mid := c.get(tool, "mid", 100, diffSBS)
	_, fresh := c.get(tool, "new", 100, diffSBS)
	if !old || mid || fresh {
		t.Errorf("after prune: old=%v mid=%v new=%v, want only the most recently used left", old, mid, fresh)
	}
}
