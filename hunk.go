package main

// hunk (https://hunk.dev) as the diff renderer. hunk has no static output: it
// is a full-screen TUI and nothing else, so it is run on a pty TALL enough to
// lay the whole patch out at once, its output goes through a terminal emulator
// and the emulated screen is read back as ANSI lines. Only its looks are used;
// it never sees a key.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

const (
	hunkMaxRows = 4000                   // the emulated screen is rows x width cells
	hunkQuiet   = 300 * time.Millisecond // no output for this long: the last frame is in
	hunkEarly   = 40 * time.Millisecond  // a pause this long after the first frame: worth showing
	hunkEarlyN  = 2048                   // bytes no terminal query sequence adds up to
	hunkTimeout = 15 * time.Second
)

// hunkFrameEnd closes a frame (synchronized output); hunk does not bracket
// every paint with it, so a pause counts as a frame's end too.
var hunkFrameEnd = []byte("\x1b[?2026l")

// hunkRows is an upper bound of the rows hunk needs for a patch: a row per
// patch line (long lines are cut, not wrapped), plus the chrome of each file.
func hunkRows(patch []byte) (rows int, capped bool) {
	rows = bytes.Count(patch, []byte("\n")) + 6*bytes.Count(patch, []byte("\ndiff --git ")) + 12
	if rows > hunkMaxRows {
		return hunkMaxRows, true
	}
	return max(rows, 24), false
}

// renderHunk renders a patch with hunk, width cells wide. hunk paints the diff
// right away and repaints as the syntax highlighting comes in, which takes a
// few times longer: early, when not nil, gets that first frame as soon as it is
// on the screen.
func renderHunk(ctx context.Context, hunkBin string, patch []byte, width int, sbs bool, early func(string)) (string, error) {
	if len(bytes.TrimSpace(patch)) == 0 {
		return "", nil
	}
	file, err := os.CreateTemp("", "hunk-*.patch")
	if err != nil {
		return "", err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(patch)
	if cerr := file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}

	mode := "unified"
	if sbs {
		mode = "split"
	}
	rows, capped := hunkRows(patch)
	ctx, cancel := context.WithTimeout(ctx, hunkTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, hunkBin, "patch", file.Name(), "--pager", "--no-sidebar", "--no-extensions",
		"--cursor-line", "off", "--no-wrap", "--mode", mode)
	// HUNK_MCP_DISABLE: or every render leaves a `hunk daemon serve` behind.
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor", "HUNK_MCP_DISABLE=1")
	tty, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: uint16(rows), Cols: uint16(width)})
	if err != nil {
		return "", err
	}
	defer tty.Close()

	em := vt.NewEmulator(width, rows)
	defer em.Close()
	go io.Copy(tty, em) // the emulator's answers to hunk's terminal queries

	var mu sync.Mutex                   // the emulator, written by the reader and read for the early frame
	var last, seen, frames atomic.Int64 // when output was last seen (0 until the first byte), how much, frames ended
	readDone := make(chan error, 1)
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := tty.Read(buf)
			if n > 0 {
				mu.Lock()
				em.Write(buf[:n])
				mu.Unlock()
				seen.Add(int64(n))
				frames.Add(int64(bytes.Count(buf[:n], hunkFrameEnd)))
				last.Store(time.Now().UnixNano())
			}
			if err != nil {
				readDone <- err
				return
			}
		}
	}()

	screen := func() string {
		mu.Lock()
		lines := strings.Split(em.Render(), "\n")
		mu.Unlock()
		for len(lines) > 0 && strings.TrimSpace(ansi.Strip(lines[len(lines)-1])) == "" {
			lines = lines[:len(lines)-1]
		}
		if capped {
			lines = append(lines, "", stDim.Render("  … cut at "+strconv.Itoa(hunkMaxRows)+" rows"))
		}
		return strings.Join(lines, "\n")
	}

	// There is no end mark, so the render is done when hunk goes quiet.
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	reading := true
wait:
	for {
		select {
		case <-ctx.Done():
			break wait
		case <-readDone: // hunk went away by itself
			reading = false
			break wait
		case <-tick.C:
			t := last.Load()
			if t == 0 {
				continue
			}
			idle := time.Since(time.Unix(0, t))
			if idle > hunkQuiet {
				break wait
			}
			if early != nil && seen.Load() > hunkEarlyN && (frames.Load() > 0 || idle > hunkEarly) {
				early(screen())
				early = nil
			}
		}
	}
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
	tty.Close()
	if reading {
		<-readDone // the emulator is not read while it is still written to
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return "", &renderError{"hunk did not finish rendering"}
		}
		return "", err
	}
	if last.Load() == 0 {
		return "", &renderError{"hunk rendered nothing"}
	}

	return screen(), nil
}
