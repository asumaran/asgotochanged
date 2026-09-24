package main

// The bubbletea model: one frame (see frame.go) holding the filter input,
// the changed files next to the diff of the one under the cursor, and the
// foot (which checkout and branch, and the panel's key). Modeled on asgitlog and
// the asgoto pickers: the input is focused before the program starts and every
// printable key filters. Enter hands the terminal to the editor and comes
// back to the list, like the fzf function this replaces.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ---- styles ----

var (
	stDim   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stTitle = lipgloss.NewStyle().Bold(true)
	stError = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	stScope = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	stCount = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	// statuses, after git's own palette
	stAdded     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	stDeleted   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	stModified  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	stUntracked = lipgloss.NewStyle().Foreground(lipgloss.Color("5"))
	stTypeChg   = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

func statusStyle(status string) lipgloss.Style {
	switch status {
	case "A", "C":
		return stAdded
	case "D":
		return stDeleted
	case "M":
		return stModified
	case "?":
		return stUntracked
	}
	return stTypeChg
}

// ---- key bindings ----

type keyMap struct {
	Nav      listNav
	Edit     key.Binding
	DiffMode key.Binding
	Space    key.Binding
	Copy     key.Binding
	Quit     key.Binding
	PrevUp   key.Binding
	PrevDown key.Binding
	Shrink   key.Binding
	Grow     key.Binding
	Filter   key.Binding
	Help     key.Binding
}

// ShortHelp is the help line: the tool's own actions, the panel's key and the
// quit keys. Moving, scrolling and resizing are in the panel, so the line
// stays short enough for a narrow popup (a cut line loses the quit keys
// first).
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Filter, k.Edit, k.DiffMode, k.Space, k.Help, k.Quit}
}

// FullHelp is the panel's list of keys, one column per group: the filter and
// the preview, the list, the tool's actions, the panel and quit.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Filter, k.PrevUp, k.Shrink},
		{k.Nav.Up, k.Nav.PageUp, k.Nav.Top},
		{k.Edit, k.DiffMode, k.Space, k.Copy},
		{k.Help, k.Quit},
	}
}

func defaultKeys() keyMap {
	return keyMap{
		Nav:      defaultListNav(),
		Edit:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
		DiffMode: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "diff mode")),
		Space:    key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^s", "whitespace")),
		Copy:     key.NewBinding(key.WithKeys("ctrl+y"), key.WithHelp("^y", "copy the path")),
		Quit:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc/q", "quit")),
		PrevUp:   key.NewBinding(key.WithKeys("shift+up"), key.WithHelp("⇧↑/⇧↓", "scroll the diff")),
		PrevDown: key.NewBinding(key.WithKeys("shift+down")),
		Shrink:   key.NewBinding(key.WithKeys("shift+left"), key.WithHelp("⇧←/⇧→", "resize the list")),
		Grow:     key.NewBinding(key.WithKeys("shift+right")),
		// Help-only entry: a binding without keys is disabled and the help
		// bubble would skip it. Nothing ever matches against it.
		Filter: key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "filter")),
		Help:   helpBinding(true),
	}
}

// ---- model ----

// editedMsg reports that the editor gave the terminal back.
type editedMsg struct{ err error }

// reloadedMsg carries the file list read again after an edit.
type reloadedMsg struct {
	ch  changes
	err error
}

type model struct {
	// data
	repo     repoInfo
	ch       changes
	loadErr  string
	deltaBin string // the renderers found at startup (difftool.go); either may be ""
	hunkBin  string
	toolPref string // the renderer chosen in the panel, remembered between runs
	diffMode string // auto | sbs | single
	ignoreWS bool   // git's -w: changes in whitespace are left out of the diffs

	rows   []fileRow
	cursor int

	// ui
	notice string // error on the help line, cleared by the next key
	flash  flash  // confirmation on the help line, cleared by a timer (flash.go)
	panel  panel  // options and keys, over the frame while it is open (panel.go)
	ti     textinput.Model
	listVP viewport.Model
	prevVP viewport.Model
	help   help.Model
	keys   keyMap
	width  int
	height int
	split  int // the preview's share of the width, percent

	// the renders: done, failed and under way (renderqueue.go)
	queue   *renderQueue[string]
	prevKey string
	dir     int // the way the cursor last moved (1, -1), for what is rendered ahead
}

func newModel(repo repoInfo, ch changes, loadErr, diffMode, hunkBin, query string) model {
	m := model{
		repo:     repo,
		ch:       ch,
		loadErr:  loadErr,
		hunkBin:  hunkBin,
		toolPref: toolHunk, // main() replaces it with the remembered one
		diffMode: diffMode,
		ignoreWS: loadIgnoreWS(),
		split:    loadSplit(stateDir()),
		ti:       newFilterInput("asgotochanged", "Search by path…"),
		listVP:   viewport.New(viewport.WithWidth(30), viewport.WithHeight(16)),
		prevVP:   viewport.New(viewport.WithWidth(60), viewport.WithHeight(16)),
		help:     help.New(),
		keys:     defaultKeys(),
		queue:    newRenderQueue[string](),
		width:    94,
		height:   24,
	}
	m.ti.SetValue(query)
	m.ti.CursorEnd()
	m.applyFilter()
	m.resize()
	m.renderList()
	return m
}

func (m *model) current() *changedFile {
	if m.cursor >= 0 && m.cursor < len(m.rows) {
		return &m.rows[m.cursor].f
	}
	return nil
}

// ---- layout ----

// innerW is the width inside the frame's sides.
func (m *model) innerW() int { return max(20, m.width-2) }

// listW is the list's share of the main section, whatever the divider leaves
// the preview.
func (m *model) listW() int { w, _ := splitWidths(m.innerW(), m.split); return w }

// detailsW is the preview's area, including the cell of padding on each
// side; prevW is the text width inside it.
func (m *model) detailsW() int { _, w := splitWidths(m.innerW(), m.split); return w }
func (m *model) prevW() int    { return max(10, m.detailsW()-2) }

// bodyH is the height of the main section: everything but the frame's own
// lines and the foot.
func (m *model) bodyH() int { return max(1, m.height-frameRows-1) }

func (m *model) resize() {
	sizePanes(&m.listVP, &m.prevVP, m.listW(), m.prevW(), m.bodyH())
	m.help.SetWidth(max(0, m.width-4))
	sizeInput(&m.ti, m.width-4)
}

// resizeList moves the divider between the list and the preview by one step.
func (m *model) resizeList(grow bool) tea.Cmd {
	m.split = moveSplit(stateDir(), m.split, grow)
	m.resize()
	m.renderList()
	return m.updatePreview()
}

// ---- filtering ----

func (m *model) applyFilter() {
	q := m.ti.Value()
	m.rows = filterFiles(m.ch.files, q)
	m.cursor = 0
	if len(m.rows) == 0 {
		m.cursor = -1
	}
}

func (m *model) keepCursorOn(path string) {
	for i, r := range m.rows {
		if r.f.path == path {
			m.cursor = i
			return
		}
	}
}

// refilter re-applies the query. With an empty query, or after the list was
// read again, the cursor stays on the file it was on.
func (m *model) refilter(keep bool) {
	path := ""
	if f := m.current(); f != nil {
		path = f.path
	}
	m.applyFilter()
	if keep || !hasTerms(m.ti.Value()) {
		m.keepCursorOn(path)
	}
}

// ---- list rendering ----

func (m *model) renderList() {
	w := m.listW()
	var b strings.Builder
	for i, r := range m.rows {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(m.fileLine(r, i == m.cursor, w))
	}
	m.listVP.SetContent(b.String())
	m.ensureVisible()
}

// fileLine renders one row: cursor bar, status letter, path. The selected row
// is padded to the full width before styling so its background spans the
// whole column.
func (m *model) fileLine(r fileRow, selected bool, width int) string {
	pathW := max(8, width-4)
	dirEnd := strings.LastIndexByte(r.f.path, '/') + 1 // the directory is dimmed
	if selected {
		line := stSel.Render("▌"+r.f.status+"  ") + pathCells(r.f.path, dirEnd, r.idx, pathW, true)
		return selPad(ansi.Truncate(line, width, ""), width)
	}
	return " " + statusStyle(r.f.status).Render(r.f.status) + "  " + pathCells(r.f.path, dirEnd, r.idx, pathW, false)
}

func (m *model) setCursor(i int) {
	if i < 0 || i >= len(m.rows) {
		return
	}
	if i != m.cursor {
		m.dir = 1
		if i < m.cursor {
			m.dir = -1
		}
	}
	m.cursor = i
}

func (m *model) ensureVisible() {
	m.listVP.SetYOffset(scrollTo(m.listVP.YOffset(), m.listVP.Height(), len(m.rows), m.cursor, m.cursor))
}

// ---- preview ----

// updatePreview refreshes the right column with the diff of the file under
// the cursor, from the renders when it is there, and renders the rows around
// it ahead of time: hunk paints a diff and highlights it a moment later, and
// rendering ahead keeps that repaint off the screen. What runs, what waits and
// what is given up is the queue's business (renderqueue.go).
func (m *model) updatePreview() tea.Cmd {
	f := m.current()
	if f == nil {
		m.prevKey = ""
		m.prevVP.SetContent("")
		return nil
	}
	key := m.keyOf(*f)
	keep := m.wanted(key)
	m.queue.cancelStale(keep)
	if key == m.prevKey && (m.queue.settled(key) || m.queue.running(key)) {
		return m.prefetch(keep) // on screen already, or about to be
	}
	if key != m.prevKey {
		m.prevVP.GotoTop()
	}
	m.prevKey = key
	if c, ok := m.queue.get(key); ok { // a partial render counts
		m.prevVP.SetContent(c)
		return m.prefetch(keep)
	}
	if msg, failed := m.queue.failed[key]; failed {
		m.prevVP.SetContent(errorBlock(msg, m.prevW()))
		return m.prefetch(keep)
	}
	m.prevVP.SetContent(stDim.Render("rendering…"))
	return m.startRender(*f, keep, true)
}

// keyOf is the render key of f as things stand (width, mode, renderer).
func (m *model) keyOf(f changedFile) string {
	return previewKey(m.repo.Top, f, m.prevW(), fileDiff(m.diffMode, m.prevW(), f), m.tool())
}

// wanted is the render keys worth having, the most wanted first: the
// selection's, then the rows around it (renderWindow).
func (m *model) wanted(key string) []string {
	keys := []string{key}
	for _, i := range renderWindow(m.cursor, m.dir) {
		if i >= 0 && i < len(m.rows) {
			keys = append(keys, m.keyOf(m.rows[i].f))
		}
	}
	return keys
}

// tool is what renders the diffs now: the remembered choice when it is
// installed.
func (m *model) tool() diffTool {
	return pickTool(m.toolPref, m.deltaBin, m.hunkBin, m.ignoreWS)
}

// options is what the panel offers, as things stand. The renderer is chosen
// there and nowhere else; the diff mode and the whitespace keep their keys.
func (m *model) options() []option { return m.diffPrefs().options() }

func (m *model) diffPrefs() diffPrefs {
	return diffPrefs{tool: m.toolPref, mode: m.diffMode, ignoreWS: m.ignoreWS}
}

// setOption changes a setting, remembers it and says so. The keys and the
// panel both come through here.
func (m *model) setOption(id string, v int) tea.Cmd {
	p := m.diffPrefs()
	flash, changed := p.set(id, v, m.deltaBin, m.hunkBin, m.prevW())
	if !changed {
		if flash == "" {
			return nil
		}
		return m.flash.fail(flash) // the option could not change
	}
	m.toolPref, m.diffMode, m.ignoreWS = p.tool, p.mode, p.ignoreWS
	switch id { // only what changed is written: a setting never chosen stays unset
	case "renderer":
		saveRenderer(m.toolPref)
	case "diff":
		saveDiffMode(m.diffMode)
	case "whitespace":
		saveIgnoreWS(m.ignoreWS)
	}
	return tea.Batch(m.setFlash(flash), m.updatePreview())
}

// startRender starts the render of f when the queue has a slot for it.
func (m *model) startRender(f changedFile, keep []string, wanted bool) tea.Cmd {
	key := m.keyOf(f)
	ctx := m.queue.start(key, keep, wanted)
	if ctx == nil {
		return nil
	}
	return renderPreviewCmd(ctx, m.repo.Top, m.ch.mergeBase, f, key, m.prevW(), fileDiff(m.diffMode, m.prevW(), f), m.tool())
}

// prefetch starts the renders of the rows around the cursor while there are
// free pipelines. It runs again whenever a render reports back, so the window
// fills up a few at a time.
func (m *model) prefetch(keep []string) tea.Cmd {
	if m.tool().plain() {
		return nil // plain git is instant: nothing to hide
	}
	var cmds []tea.Cmd
	for _, i := range renderWindow(m.cursor, m.dir) {
		if !m.queue.free() {
			break
		}
		if i < 0 || i >= len(m.rows) {
			continue
		}
		if f := m.rows[i].f; !m.queue.settled(m.keyOf(f)) {
			cmds = append(cmds, m.startRender(f, keep, false))
		}
	}
	return tea.Batch(cmds...)
}

func (m *model) setFlash(s string) tea.Cmd { return m.flash.set(s) }

// ---- editing ----

// editorArgv is the editor command. ASGOTOCHANGED_OPENER replaces it
// (opener.go); the default is nvim, what the fzf function opened, then
// $EDITOR, then vi.
func editorArgv() []string {
	if f := openerArgv("asgotochanged"); len(f) > 0 {
		return f
	}
	if _, err := exec.LookPath("nvim"); err == nil {
		return []string{"nvim"}
	}
	if f := strings.Fields(os.Getenv("EDITOR")); len(f) > 0 {
		return f
	}
	return []string{"vi"}
}

// edit hands the terminal to the editor on the file under the cursor. Quitting
// the editor comes back to the list. A deleted file has nothing to open.
func (m *model) edit() tea.Cmd {
	f := m.current()
	if f == nil {
		return nil
	}
	path := filepath.Join(m.repo.Top, f.path)
	if _, err := os.Stat(path); err != nil {
		m.notice = "nothing to edit: " + f.path + " does not exist in the work tree"
		return nil
	}
	argv := append(editorArgv(), path)
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = m.repo.Top
	return tea.ExecProcess(cmd, func(err error) tea.Msg { return editedMsg{err} })
}

func reloadCmd() tea.Cmd {
	return func() tea.Msg {
		ch, err := loadChanges(context.Background())
		return reloadedMsg{ch, err}
	}
}

// ---- bubbletea ----

// Init starts no preview: the size is not known yet, and a render at a made-up
// width is one nobody sees that holds a slot. The first tea.WindowSizeMsg
// starts it, as in asgitlog.
func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		m.renderList()
		return m, m.updatePreview()

	case previewMsg:
		m.queue.report(msg.key, msg.content, msg.partial, msg.next == nil, msg.cancelled, msg.err)
		if c, ok := m.queue.get(msg.key); ok && msg.key == m.prevKey && !msg.cancelled {
			// The final frame replaces the partial one in place: same lines,
			// now highlighted, so the scroll position is kept.
			y := m.prevVP.YOffset()
			m.prevVP.SetContent(c)
			m.prevVP.SetYOffset(y)
		} else if msg.key == m.prevKey && msg.err != nil {
			m.prevKey = "" // updatePreview shows the failure
		}
		return m, tea.Batch(msg.next, m.updatePreview())

	case editedMsg:
		if msg.err != nil {
			m.notice = "editor: " + msg.err.Error()
		}
		// The edit may have changed the diff, or the list itself.
		return m, reloadCmd()

	case reloadedMsg:
		if msg.err != nil {
			m.notice = msg.err.Error()
			return m, nil
		}
		m.ch = msg.ch
		m.refilter(true)
		m.renderList()
		m.prevKey = ""
		return m, m.updatePreview()

	case flashMsg:
		return m, m.setFlash(string(msg))

	case flashErrMsg:
		return m, m.flash.fail(string(msg))

	case clearFlashMsg:
		m.flash.clear(msg)
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		if m.panel.open {
			return m, nil
		}
		// Over the list the wheel moves the selection, as in asgitlog; anywhere
		// else it scrolls the preview.
		if m.overList(msg.X, msg.Y) {
			if k, ok := wheelKey(msg); ok {
				return m.handleKey(k)
			}
			return m, nil
		}
		m.prevVP, _ = m.prevVP.Update(msg)
		return m, nil

	case tea.MouseClickMsg:
		if m.panel.open {
			return m, nil
		}
		return m.handleClick(msg)

	case tea.PasteMsg:
		if m.panel.open {
			return m, nil // nothing is typed under the panel
		}
		return m.toInput(msg)

	default:
		// Whatever else the input takes (its own paste, the cursor's blink).
		return m.toInput(msg)
	}
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case m.panel.open:
		// The panel takes every key: esc closes it before it quits anything.
		if a := m.panel.update(msg, m.options()); a.id != "" {
			return m, m.setOption(a.id, a.value)
		}
		return m, nil
	case isHelpKey(msg):
		m.panel.toggle()
		return m, nil
	case msg.String() == "q" && m.ti.Value() == "":
		// q quits only while the filter is empty; otherwise it is text.
		return m, tea.Quit
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Edit):
		return m, m.edit()
	case key.Matches(msg, m.keys.DiffMode):
		return m, m.setOption("diff", nextValue(m.options(), "diff"))
	case key.Matches(msg, m.keys.Space):
		return m, m.setOption("whitespace", nextValue(m.options(), "whitespace"))
	case key.Matches(msg, m.keys.Copy):
		if f := m.current(); f != nil {
			return m, copyCmd("asgotochanged", "", f.path)
		}
		return m, copyCmd("asgotochanged", "", "")
	case m.keys.Nav.matches(msg):
		m.setCursor(m.keys.Nav.move(msg, m.cursor, len(m.rows), m.listVP.Height(), nil))
		m.renderList()
		return m, m.updatePreview()
	case key.Matches(msg, m.keys.Shrink):
		return m, m.resizeList(false)
	case key.Matches(msg, m.keys.Grow):
		return m, m.resizeList(true)
	case key.Matches(msg, m.keys.PrevUp):
		m.prevVP.ScrollUp(3)
		return m, nil
	case key.Matches(msg, m.keys.PrevDown):
		m.prevVP.ScrollDown(3)
		return m, nil
	}

	return m.toInput(msg)
}

// toInput hands a message to the filter input and, when that changed the
// query, filters again: a key, a paste from the terminal (tea.PasteMsg) or the
// input's own ctrl+v all come through here, so the list never lags behind
// what the input shows. A message that leaves the query alone moves nothing.
func (m model) toInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd, changed := typeInto(&m.ti, msg)
	if !changed {
		return m, cmd
	}
	m.refilter(false)
	m.renderList()
	return m, tea.Batch(cmd, m.updatePreview())
}

// overList reports whether a screen cell is inside the list.
func (m *model) overList(x, y int) bool {
	return inList(x, y, listY, m.listW(), m.bodyH())
}

// handleClick moves the cursor to the row under a left click on the list. It
// never opens the editor: that stays on enter.
func (m model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || !m.overList(msg.X, msg.Y) {
		return m, nil
	}
	i, ok := rowUnder(msg.Y, listY, m.listVP.YOffset(), len(m.rows))
	if !ok || i == m.cursor {
		return m, nil
	}
	m.setCursor(i)
	m.renderList()
	return m, m.updatePreview()
}

func (m model) View() tea.View { return popupView(m.render(), true) }

// render stacks the sections in one frame (see frame.go). The foot carries
// the one thing the rest of the screen cannot say: which checkout and branch.
func (m model) render() string {
	w := m.width
	out := frameHead(w, withDevMark(m.status()), m.ti.View())
	out = append(out, splitMain(m.listLines(), strings.Split(m.prevVP.View(), "\n"),
		m.listW(), m.detailsW(), m.counter(), diffEdge(m.ignoreWS, scrollPos(&m.prevVP)))...)
	out = append(out, framed(w, footLine(m.flash, m.notice, m.context(), m.help, m.keys, w-4)), hline(w, "╰", "╯", "", ""))
	if m.panel.open {
		keys := keyLines(m.help, m.keys, w-10)
		out = overlay(out, panelLines(m.options(), m.panel.cursor, keys, w-4, len(out)-2), w)
	}
	return strings.Join(out, "\n")
}

// context is the repository summary for the foot, fitted to the room the
// panel's key leaves.
func (m model) context() string {
	return stInfo.Render(m.repo.line(footRoom(m.help, m.keys, m.width-4)))
}

// counter is the matches/total count, for the edge under the list.
func (m model) counter() string {
	return stCount.Render(strconv.Itoa(len(m.rows)) + "/" + strconv.Itoa(len(m.ch.files)))
}

// status is what the branch is compared against (the scope, as in asgitlog),
// for the edge over the input.
func (m model) status() string {
	if m.ch.base == "" {
		return ""
	}
	return stScope.Render("[vs " + truncate(m.ch.base, max(10, m.width/3)) + "]")
}

// listLines is the list as exactly bodyH lines of listW cells.
func (m model) listLines() []string { return fitLines(m.leftColumn(), m.bodyH(), m.listW()) }

// leftColumn is the list, or the reason there is nothing to list.
func (m model) leftColumn() string {
	if len(m.rows) > 0 {
		return m.listVP.View()
	}
	return emptyList(m.loadErr, m.ti.Value(), "No changes vs "+m.ch.base, m.listW())
}
