#!/usr/bin/env python3
"""End-to-end TUI check for asgotochanged without a real terminal.

Spawns the binary on a pty, answers the terminal queries bubbletea sends,
replays keystrokes and asserts on frames rendered with pyte. Everything runs
in a throwaway sandbox: a fake HOME, a real git checkout with a local `main`
as the base and a feature branch (modified, added, deleted, pending and
untracked files), hunk turned off (ASGOTOCHANGED_HUNK=none: git's own diff, so
the frames do not depend on hunk's looks) and a stub instead of the editor
(ASGOTOCHANGED_EDITOR) that logs the path and appends a line to the file. It
never touches a real repository and never opens an editor.

Usage: scripts/pty-check.py ./asgotochanged   (needs python3 + pyte)
"""
NAME, ROWS, COLS = "asgotochanged", 18, 150
import atexit, fcntl, json, os, pty, select, shutil, signal, struct, subprocess, sys, tempfile, termios, time
import pyte

BIN = os.path.abspath(sys.argv[1])
SANDBOX = os.path.realpath(tempfile.mkdtemp(prefix="%s-pty-" % NAME))
home = os.path.join(SANDBOX, "home")
os.makedirs(home)

def write(path, text, mode=None):
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write(text)
    if mode: os.chmod(path, mode)
    return path

QUERIES = [(b"\x1b]11;?", b"\x1b]11;rgb:0000/0000/0000\x1b\\"), (b"\x1b]10;?", b"\x1b]10;rgb:ffff/ffff/ffff\x1b\\"),
           (b"\x1b[6n", b"\x1b[1;1R"), (b"\x1b[c", b"\x1b[?62c")]

failures = []
def check(cond, msg):
    print(("  ok   " if cond else "  FAIL ") + msg)
    if not cond: failures.append(msg)

class Session:
    """One run of the binary on a pty."""
    def __init__(self, env, args=(), cwd=None):
        self.master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, COLS, 0, 0))
        self.proc = subprocess.Popen([BIN, *args], stdin=slave, stdout=slave, stderr=slave, env=env,
                                     close_fds=True, cwd=cwd or SANDBOX)
        os.close(slave)
        # A failed assertion must not leave the binary running on a dead pty.
        atexit.register(lambda p=self.proc: p.poll() is None and p.kill())
        self.screen = pyte.Screen(COLS, ROWS)
        self.stream = pyte.ByteStream(self.screen)
        self.raw = bytearray()
        self.answered = 0

    def pump(self, seconds):
        end = time.time() + seconds
        while True:
            left = end - time.time()
            if left <= 0: break
            r, _, _ = select.select([self.master], [], [], left)
            if not r: continue
            try:
                data = os.read(self.master, 65536)
            except OSError:
                break
            if not data: break
            self.raw.extend(data); self.stream.feed(data)
            tail = bytes(self.raw[self.answered:])
            for q, reply in QUERIES:
                for _ in range(tail.count(q)):
                    os.write(self.master, reply)
            self.answered = len(self.raw)

    def repaint(self):
        # The v2 renderer updates the screen with scroll regions and SU, which
        # pyte ignores; a resize forces a full redraw it can follow.
        for cols in (COLS - 1, COLS):
            fcntl.ioctl(self.master, termios.TIOCSWINSZ, struct.pack("HHHH", ROWS, cols, 0, 0))
            self.screen.resize(ROWS, cols)
            if self.proc.poll() is None: os.kill(self.proc.pid, signal.SIGWINCH)
            self.pump(0.3)

    def frame(self):
        return [line.rstrip() for line in self.screen.display]

    def send(self, b, wait=0.4):
        os.write(self.master, b); self.pump(wait); self.repaint()
        return self.frame()

    def start(self, marker):
        for _ in range(50):
            self.pump(0.1)
            if marker in "\n".join(self.frame()): break
        self.pump(0.5); self.repaint()
        return self.frame()

    def finish(self):
        try:
            self.proc.wait(timeout=3)
        except subprocess.TimeoutExpired:
            self.proc.kill()
            return None
        self.pump(0.2)
        return self.proc.returncode

def dump(title, f):
    print("--- %s ---" % title)
    for i, l in enumerate(f): print("%2d|%s" % (i, l))

def done():
    shutil.rmtree(SANDBOX, ignore_errors=True)
    print("\n%d failure(s)" % len(failures))
    sys.exit(1 if failures else 0)

CTRL_A, CTRL_S, CTRL_T, ESC, ENTER, TAB, DOWN, UP = b"\x01", b"\x13", b"\x14", b"\x1b", b"\r", b"\t", b"\x1b[B", b"\x1b[A"

# ---------- sandbox: git checkout, editor stub ----------
repo = os.path.join(home, "wt", "shop", "fix-cart-total")
os.makedirs(repo)
git_env = dict(os.environ, GIT_CONFIG_GLOBAL="/dev/null", GIT_CONFIG_SYSTEM="/dev/null", HOME=home)
def git(*args):
    subprocess.run(["git", "-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", *args],
                   check=True, capture_output=True, env=git_env)
git("init", "-q", "-b", "main")
write(os.path.join(repo, "src", "total.ts"), "export const total = (items) => items.length\n")
write(os.path.join(repo, "src", "old.ts"), "export const old = 1\n")
git("add", "."); git("commit", "-q", "-m", "base")
git("checkout", "-q", "-b", "fix/cart-total")
write(os.path.join(repo, "src", "total.ts"), "export const total = (items) => items.reduce(sum, 0)\n")
write(os.path.join(repo, "src", "tax.ts"), "export const tax = 0.21\n")
git("rm", "-q", "src/old.ts"); git("add", "."); git("commit", "-q", "-m", "work")
write(os.path.join(repo, "notes", "PLAN.md"), "# plan\n")

edit_log = os.path.join(SANDBOX, "edit.log")
editor = write(os.path.join(SANDBOX, "editor"),
               '#!/bin/sh\nprintf "%%s\\n" "$1" >> "%s"\nprintf "// edited\\n" >> "$1"\n' % edit_log, 0o755)

def session(args=()):
    env = dict(git_env, TERM="xterm-256color", COLORTERM="truecolor", ASGOTOCHANGED_HUNK="none",
               ASGOTOCHANGED_EDITOR=editor, XDG_CONFIG_HOME=os.path.join(home, ".config"),
               XDG_CACHE_HOME=os.path.join(home, ".cache"))
    for k in ("HERDR_PLUGIN_STATE_DIR", "HERDR_PLUGIN_ENTRYPOINT_ID", "HERDR_PLUGIN_CONTEXT_JSON"):
        env.pop(k, None)
    if os.path.exists(edit_log): os.remove(edit_log)
    return Session(env, args=args, cwd=os.path.join(repo, "src"))   # a subdirectory: paths stay relative to the top

def edited():
    return open(edit_log).read().splitlines() if os.path.exists(edit_log) else []

# One frame (see frame.go): border, context, counter edge, input, main edge,
# list | preview, bottom edge, help, border.
def listw(): return max(COLS - 3 - (COLS - 2) * 75 // 100, 10)   # the default split: list 25%, preview 75%
def divider(f): return f[5].index("│", 1)
def left(f):  return [l[1:1 + listw()].rstrip() for l in f[5:-3] if l[1:1 + listw()].strip()]
def right(f): return "\n".join(l[listw() + 3:-1].rstrip() for l in f[5:-3])
def prompt(f): return f[3].strip("│ ").rstrip()
def counter(f): return f[2].strip("├┤─ ")

print("== asgotochanged pty driver (%dx%d) ==" % (COLS, ROWS))

# ---------- run 1: list, context, diff, filter, edit and come back ----------
s = session()
f = s.start("asgotochanged (dev) ❯"); dump("open", f)
check(prompt(f) == "asgotochanged (dev) ❯", "prompt line is clean: %r" % f[3])
check(b"\x1b[?1049h" in s.raw, "program entered the alt screen")
check("~/wt/shop/fix-cart-total  fix/cart-total" in f[1], "the context line names the checkout and the branch: %r" % f[1])
check(counter(f) == "4/4 [vs main]", "counter and base on the edge: %r" % counter(f))
rows = left(f)
check(rows == ["▌?  notes/PLAN.md", " D  src/old.ts", " A  src/tax.ts", " M  src/total.ts"], "changed files by path, with their status: %r" % rows)
check("+# plan" in right(f), "an untracked file is diffed against nothing")
f = s.send(b"total", 0.8); dump("filtered", f)
check(left(f) == ["▌M  src/total.ts"] and counter(f).startswith("1/4"), "typing filters: %r" % left(f))
check("-export const total = (items) => items.length" in right(f) and "+export const total = (items) => items.reduce(sum, 0)" in right(f),
      "the diff is against the merge base")
f = s.send(ENTER, 1.2); dump("after the editor", f)
check(edited() == [os.path.join(repo, "src", "total.ts")], "enter hands the file to the editor: %r" % edited())
check(s.proc.poll() is None and prompt(f) == "asgotochanged (dev) ❯ total", "quitting the editor comes back to the list: %r" % f[3])
check("+// edited" in right(f), "the diff is rendered again after the edit")

# a deleted file has nothing to edit
for _ in range(5): s.send(b"\x7f", 0.1)
f = s.send(b"old", 0.6)
check("▌D  src/old.ts" in left(f), "the cursor lands on the best match, the deleted file: %r" % left(f))
f = s.send(ENTER, 0.6)
check(s.proc.poll() is None and "nothing to edit" in f[-2] and len(edited()) == 1, "a deleted file is not handed to the editor: %r" % f[-2])
f = s.send(CTRL_T, 0.6)
check("diff: side-by-side" in f[-2], "ctrl+t cycles the diff mode and says so: %r" % f[-2])
os.write(s.master, ESC); s.pump(0.4)
check(s.finish() == 0, "esc quits")

# ---------- run 2: mouse, initial query, q ----------
s = session(args=("tax",))
f = s.start("asgotochanged (dev) ❯")
check(prompt(f) == "asgotochanged (dev) ❯ tax" and left(f) == ["▌A  src/tax.ts"], "a positional argument is the initial filter: %r" % left(f))
for _ in range(3): s.send(b"\x7f", 0.1)
f = s.send(b"\x1b[<0;5;8M\x1b[<0;5;8m", 0.6)   # SGR press+release on the third list line
check(left(f)[2].startswith("▌A  src/tax.ts") and edited() == [], "a click selects the row and opens nothing: %r" % left(f))
os.write(s.master, b"q"); s.pump(0.4)
check(s.finish() == 0, "q quits with an empty filter")

# ---------- run 3: the divider moves and stays where it was left ----------
s = session()
f = s.start("asgotochanged (dev) ❯"); at = divider(f)
f = s.send(b"\x1b[1;2C", 0.6); grown = divider(f)   # shift+right
check(grown > at and all(len(l) == COLS for l in f), "shift+right grows the list: %d -> %d" % (at, grown))
f = s.send(b"\x1b[1;2D", 0.6)                        # shift+left
check(divider(f) == at, "shift+left shrinks it back: %d" % divider(f))
s.send(b"\x1b[1;2C", 0.6)
os.write(s.master, ESC); s.pump(0.4); s.finish()
s = session()
f = s.start("asgotochanged (dev) ❯")
check(divider(f) == grown, "the next run opens with the same split: %d" % divider(f))
s.send(b"\x1b[1;2D", 0.6)
os.write(s.master, ESC); s.pump(0.4); s.finish()

# ---------- run 4: nothing changed on the base branch ----------
git("stash", "-q", "-u"); git("checkout", "-q", "main")
s = session()
f = s.start("asgotochanged (dev) ❯"); dump("on the base", f)
check(counter(f) == "0/0 [vs main]" and "No changes vs main" in "\n".join(f), "the base branch has nothing to list")
os.write(s.master, ESC); s.pump(0.4); s.finish()

done()
