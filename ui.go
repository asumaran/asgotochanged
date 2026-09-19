package main

// The bubbletea model: one frame (see frame.go) holding the context line
// (which checkout and branch), the filter input, the changed files next to
// the diff of the one under the cursor, and the help. Modeled on asgitlog and
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
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func truncate(s string, width int) string {
	return ansi.Truncate(s, width, "…")
}

// ---- styles ----

var (
	stPrompt = lipgloss.NewStyle().Foreground(lipgloss.Color("13")).Bold(true)
	stDev    = lipgloss.NewStyle().Foreground(lipgloss.Color("208")).Bold(true)
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	stTitle  = lipgloss.NewStyle().Bold(true)
	stError  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	stInfo   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	stScope  = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	stCount  = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	stFlash  = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))

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
	Quit     key.Binding
	PrevUp   key.Binding
	PrevDown key.Binding
	Shrink   key.Binding
	Grow     key.Binding
	Filter   key.Binding
	Help     key.Binding
}

// ShortHelp is the folded help line: the tool's own actions, the help and the
// quit keys. Moving, scrolling and resizing are in the expanded help, so the
// line stays short enough for a narrow popup (a cut line loses the quit keys
// first).
func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Filter, k.Edit, k.DiffMode, k.Space, k.Help, k.Quit}
}

// FullHelp is what `?` expands the help into, one column per group: the
// filter and the preview, the list, the tool's actions, help and quit.
func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Filter, k.PrevUp, k.Shrink},
		{k.Nav.Up, k.Nav.PageUp, k.Nav.Top},
		{k.Edit, k.DiffMode, k.Space},
		{k.Help, k.Quit},
	}
}

func defaultKeys() keyMap {
	return keyMap{
		Nav:      defaultListNav(),
		Edit:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "edit")),
		DiffMode: key.NewBinding(key.WithKeys("ctrl+t"), key.WithHelp("^t", "diff mode")),
		Space:    key.NewBinding(key.WithKeys("ctrl+s"), key.WithHelp("^s", "whitespace")),
		Quit:     key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc/q", "quit")),
		PrevUp:   key.NewBinding(key.WithKeys("shift+up"), key.WithHelp("⇧↑/⇧↓", "scroll the diff")),
		PrevDown: key.NewBinding(key.WithKeys("shift+down")),
		Shrink:   key.NewBinding(key.WithKeys("shift+left"), key.WithHelp("⇧←/⇧→", "resize the list")),
		Grow:     key.NewBinding(key.WithKeys("shift+right")),
		// Help-only entry: a binding without keys is disabled and the help
		// bubble would skip it. Nothing ever matches against it.
		Filter: key.NewBinding(key.WithKeys("type"), key.WithHelp("type", "filter")),
		Help:   helpKey,
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

type clearFlashMsg int

type model struct {
	// data
	repo     repoInfo
	ch       changes
	loadErr  string
	hunkBin  string
	diffMode string // auto | sbs | single
	ignoreWS bool   // git's -w: changes in whitespace are left out of the diffs

	rows   []fileRow
	cursor int

	// ui
	notice string // error on the help line, cleared by the next key
	flash  string // confirmation on the help line, cleared by a timer
	flashN int
	ti     textinput.Model
	listVP viewport.Model
	prevVP viewport.Model
	help   help.Model
	keys   keyMap
	width  int
	height int
	split  int // the preview's share of the width, percent

	// preview render cache
	renders map[string]string
	prevKey string
	// inflight is the renders running, by key: the selected file's and the
	// ones rendered ahead. Each carries the cancel of its context.
	inflight map[string]context.CancelFunc
}

func newModel(repo repoInfo, ch changes, loadErr, diffMode, hunkBin, query string) model {
	m := model{
		repo:     repo,
		ch:       ch,
		loadErr:  loadErr,
		hunkBin:  hunkBin,
		diffMode: diffMode,
		ignoreWS: loadIgnoreWS(),
		split:    loadSplit(stateDir()),
		ti:       newFilterInput(),
		listVP:   viewport.New(viewport.WithWidth(30), viewport.WithHeight(16)),
		prevVP:   viewport.New(viewport.WithWidth(60), viewport.WithHeight(16)),
		help:     help.New(),
		keys:     defaultKeys(),
		renders:  map[string]string{},
		inflight: map[string]context.CancelFunc{},
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

// newFilterInput builds the focused filter textinput with the asgotochanged
// prompt. The prompt string already carries its colors, so the prompt style
// is left empty.
func newFilterInput() textinput.Model {
	ti := textinput.New()
	ti.Prompt = promptText()
	st := ti.Styles()
	st.Focused.Prompt = lipgloss.NewStyle()
	st.Blurred.Prompt = lipgloss.NewStyle()
	ti.SetStyles(st)
	ti.Focus()
	return ti
}

// promptText builds the textinput prompt, with an orange "(dev)" marker on
// non-release builds.
func promptText() string {
	if strings.HasPrefix(version, "v") {
		return stPrompt.Render("asgotochanged ❯ ")
	}
	return stPrompt.Render("asgotochanged (") + stDev.Render("dev") + stPrompt.Render(") ❯ ")
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
// lines (with the context line) and the help.
func (m *model) bodyH() int { return max(1, m.height-frameRows(true)-m.footH()) }

// footH is the height of the foot: a message takes one line, the help more
// while `?` has it expanded; the main section keeps at least minBodyH.
func (m *model) footH() int {
	if m.footMsg() != "" {
		return 1
	}
	return helpHeight(m.help, m.keys, m.height-frameRows(true)-minBodyH)
}

const minBodyH = 4

func (m *model) toggleHelp() tea.Cmd {
	m.help.ShowAll = !m.help.ShowAll
	m.resize()
	m.renderList()
	return m.updatePreview()
}

func (m *model) resize() {
	m.listVP.SetWidth(m.listW())
	m.listVP.SetHeight(m.bodyH())
	m.prevVP.SetWidth(m.prevW())
	m.prevVP.SetHeight(m.bodyH())
	m.help.SetWidth(max(0, m.width-4))
}

// resizeList moves the divider between the list and the preview by one step.
func (m *model) resizeList(grow bool) tea.Cmd {
	m.split = stepSplit(m.split, grow)
	saveSplit(stateDir(), m.split)
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
	if keep || m.ti.Value() == "" {
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
	m.cursor = i
}

func (m *model) ensureVisible() {
	h, c := m.listVP.Height(), m.cursor
	if h <= 0 || c < 0 {
		m.listVP.SetYOffset(0)
		return
	}
	if c < m.listVP.YOffset() {
		m.listVP.SetYOffset(c)
	} else if c >= m.listVP.YOffset()+h {
		m.listVP.SetYOffset(c - h + 1)
	}
}

// ---- preview ----

const (
	// maxPipelines bounds the renders running at once, so holding an arrow
	// key never piles up hunk processes.
	maxPipelines = 3
	// prefetchAround is how many rows on each side of the cursor are rendered
	// ahead, nearest first. hunk paints a diff and highlights it a moment
	// later; rendering ahead keeps that repaint off the screen, so moving
	// through the list shows finished diffs, as asgitlog does.
	prefetchAround = 6
)

// updatePreview refreshes the right column with the diff of the file under
// the cursor, from the render cache when it is there, and renders the rows
// around it ahead of time.
func (m *model) updatePreview() tea.Cmd {
	f := m.current()
	if f == nil {
		m.prevKey = ""
		m.prevVP.SetContent("")
		return nil
	}
	mode := fileDiff(m.diffMode, m.prevW(), *f)
	key := previewKey(m.repo.Top, *f, m.prevW(), mode, m.ignoreWS)
	if key == m.prevKey {
		return m.prefetch()
	}
	m.prevKey = key
	m.prevVP.GotoTop()
	if c, ok := m.renders[key]; ok {
		m.prevVP.SetContent(c)
		return m.prefetch()
	}
	m.prevVP.SetContent(stDim.Render("rendering…"))
	if _, running := m.inflight[key]; running {
		return nil // rendered ahead and about to report
	}
	if len(m.inflight) >= maxPipelines {
		m.cancelFarthest() // the selection never waits for a slot
	}
	return m.startRender(*f, key, mode)
}

func (m *model) startRender(f changedFile, key, mode string) tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.inflight[key] = cancel
	return renderPreviewCmd(ctx, m.repo.Top, m.ch.mergeBase, f, key, m.prevW(), mode, m.ignoreWS, m.hunkBin)
}

// around is the rows worth having rendered besides the selected one, nearest
// first, the row below before the row above.
func (m *model) around() []int {
	var rows []int
	for d := 1; d <= prefetchAround; d++ {
		for _, i := range []int{m.cursor + d, m.cursor - d} {
			if i >= 0 && i < len(m.rows) {
				rows = append(rows, i)
			}
		}
	}
	return rows
}

// prefetch starts the renders of the rows around the cursor while there are
// free pipelines. It runs again whenever a render reports back, so the window
// fills up a few at a time.
func (m *model) prefetch() tea.Cmd {
	if m.hunkBin == "" {
		return nil // plain git is instant: nothing to hide
	}
	var cmds []tea.Cmd
	for _, i := range m.around() {
		if len(m.inflight) >= maxPipelines {
			break
		}
		f := m.rows[i].f
		mode := fileDiff(m.diffMode, m.prevW(), f)
		key := previewKey(m.repo.Top, f, m.prevW(), mode, m.ignoreWS)
		if _, ok := m.renders[key]; ok {
			continue
		}
		if _, ok := m.inflight[key]; ok {
			continue
		}
		cmds = append(cmds, m.startRender(f, key, mode))
	}
	return tea.Batch(cmds...)
}

// cancelFarthest gives up the render ahead that is least likely to be wanted:
// one that is no longer around the cursor, else the farthest one.
func (m *model) cancelFarthest() {
	keep := map[string]int{}
	for rank, i := range m.around() {
		f := m.rows[i].f
		keep[previewKey(m.repo.Top, f, m.prevW(), fileDiff(m.diffMode, m.prevW(), f), m.ignoreWS)] = rank
	}
	victim, worst := "", -1
	for key := range m.inflight {
		rank, ok := keep[key]
		if !ok {
			victim = key
			break
		}
		if rank > worst {
			victim, worst = key, rank
		}
	}
	if cancel := m.inflight[victim]; cancel != nil {
		cancel()
		delete(m.inflight, victim)
	}
}

// flashFor is how long a confirmation stays on the help line.
const flashFor = 1500 * time.Millisecond

func (m *model) setFlash(s string) tea.Cmd {
	m.flash = s
	m.flashN++
	n := m.flashN
	return tea.Tick(flashFor, func(time.Time) tea.Msg { return clearFlashMsg(n) })
}

// ---- editing ----

// editorArgv is the editor command. ASGOTOCHANGED_EDITOR replaces it; the
// default is nvim, what the fzf function opened, then $EDITOR, then vi.
func editorArgv() []string {
	if f := strings.Fields(os.Getenv("ASGOTOCHANGED_EDITOR")); len(f) > 0 {
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

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.updatePreview())
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.resize()
		m.renderList()
		return m, m.updatePreview()

	case previewMsg:
		if msg.next == nil { // final, failed or cancelled: the pipeline is over
			if cancel := m.inflight[msg.key]; cancel != nil {
				cancel()
				delete(m.inflight, msg.key)
			}
		}
		if msg.cancelled {
			return m, m.updatePreview()
		}
		if !msg.partial {
			m.renders[msg.key] = msg.content
		}
		if msg.key == m.prevKey {
			// The final frame replaces the partial one in place: same lines,
			// now highlighted, so the scroll position is kept.
			y := m.prevVP.YOffset()
			m.prevVP.SetContent(msg.content)
			m.prevVP.SetYOffset(y)
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

	case clearFlashMsg:
		if int(msg) == m.flashN {
			m.flash = ""
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case tea.MouseWheelMsg:
		// Over the list the wheel moves the selection, as in asgitlog; anywhere
		// else it scrolls the preview.
		if m.overList(msg.X, msg.Y) {
			switch msg.Button {
			case tea.MouseWheelUp:
				return m.handleKey(tea.KeyPressMsg{Code: tea.KeyUp})
			case tea.MouseWheelDown:
				return m.handleKey(tea.KeyPressMsg{Code: tea.KeyDown})
			}
			return m, nil
		}
		m.prevVP, _ = m.prevVP.Update(msg)
		return m, nil

	case tea.MouseClickMsg:
		return m.handleClick(msg)

	default:
		var cmd tea.Cmd
		m.ti, cmd = m.ti.Update(msg)
		return m, cmd
	}
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.notice = ""
	switch {
	case msg.String() == "ctrl+c":
		return m, tea.Quit
	case foldsHelp(msg, m.help):
		return m, m.toggleHelp() // esc folds the help before it quits
	case isHelpKey(msg, m.ti.Value()):
		return m, m.toggleHelp()
	case msg.String() == "q" && m.ti.Value() == "":
		// q quits only while the filter is empty; otherwise it is text.
		return m, tea.Quit
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.Edit):
		return m, m.edit()
	case key.Matches(msg, m.keys.DiffMode):
		m.diffMode = nextDiffMode(m.diffMode)
		saveDiffMode(m.diffMode)
		return m, tea.Batch(m.setFlash("diff: "+diffLabel(m.diffMode, m.prevW())), m.updatePreview())
	case key.Matches(msg, m.keys.Space):
		m.ignoreWS = !m.ignoreWS
		saveIgnoreWS(m.ignoreWS)
		return m, tea.Batch(m.setFlash("whitespace: "+wsLabel(m.ignoreWS)), m.updatePreview())
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

	before := m.ti.Value()
	var cmd tea.Cmd
	m.ti, cmd = m.ti.Update(msg)
	if m.ti.Value() != before {
		m.refilter(false)
		m.renderList()
	}
	return m, tea.Batch(cmd, m.updatePreview())
}

// overList reports whether a screen cell is inside the list.
func (m *model) overList(x, y int) bool {
	return x >= 1 && x <= m.listW() && y >= listY(true) && y < listY(true)+m.bodyH()
}

// handleClick moves the cursor to the row under a left click on the list. It
// never opens the editor: that stays on enter.
func (m model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Button != tea.MouseLeft || !m.overList(msg.X, msg.Y) {
		return m, nil
	}
	i := msg.Y - listY(true) + m.listVP.YOffset()
	if i < 0 || i >= len(m.rows) || i == m.cursor {
		return m, nil
	}
	m.setCursor(i)
	m.renderList()
	return m, m.updatePreview()
}

func (m model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

// render stacks the sections in one frame (see frame.go). The context line is
// the one thing the rest of the screen cannot say: which checkout and branch.
func (m model) render() string {
	w := m.width
	out := frameHead(w, stInfo.Render(m.repo.line(max(0, w-4))), m.counter(), m.ti.View())
	out = append(out, splitMain(m.listLines(), strings.Split(m.prevVP.View(), "\n"),
		m.listW(), m.detailsW(), listPos(&m.listVP, nil), diffEdge(m.ignoreWS, scrollPos(&m.prevVP)))...)
	for _, l := range m.footLines() {
		out = append(out, framed(w, l))
	}
	out = append(out, hline(w, "╰", "╯", "", ""))
	return strings.Join(out, "\n")
}

// counter is the matches/total count, followed by what the branch is compared
// against (the scope, as in asgitlog).
func (m model) counter() string {
	s := stCount.Render(strconv.Itoa(len(m.rows)) + "/" + strconv.Itoa(len(m.ch.files)))
	if m.ch.base != "" {
		s += " " + stScope.Render("[vs "+truncate(m.ch.base, max(10, m.width/3))+"]")
	}
	return s
}

// listLines is the list as exactly bodyH lines of listW cells.
func (m model) listLines() []string {
	lines := strings.Split(m.leftColumn(), "\n")
	for len(lines) < m.bodyH() {
		lines = append(lines, "")
	}
	lines = lines[:m.bodyH()]
	for i, l := range lines {
		lines[i] = fit(l, m.listW())
	}
	return lines
}

// leftColumn is the list, or the reason there is nothing to list.
func (m model) leftColumn() string {
	if len(m.rows) > 0 {
		return m.listVP.View()
	}
	msg := "No matches"
	switch {
	case m.loadErr != "":
		msg = m.loadErr
	case m.ti.Value() != "":
	default:
		msg = "No changes vs " + m.ch.base
	}
	return stDim.Render(truncate(" "+msg, m.listW()))
}

// diffEdge is what the main section's bottom edge says about the diff: a mark
// while git's -w is on, and the scroll position.
func diffEdge(ignoreWS bool, pos string) string {
	if !ignoreWS {
		return pos
	}
	mark := stScope.Render("[-w]")
	if pos == "" {
		return mark
	}
	return mark + stDim.Render(" ─ ") + pos
}

// footer is the key help, or a notice or confirmation while one is showing.
// footMsg is what takes the help's place while there is something to say.
func (m model) footMsg() string {
	switch {
	case m.notice != "":
		return stError.Render(truncate(m.notice, max(0, m.width-4)))
	case m.flash != "":
		return stFlash.Render(truncate(m.flash, max(0, m.width-4)))
	}
	return ""
}

func (m model) footLines() []string {
	if msg := m.footMsg(); msg != "" {
		return []string{msg}
	}
	return helpLines(m.help, m.keys, m.width-4, m.footH())
}
