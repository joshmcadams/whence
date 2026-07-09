// Package tui is the interactive terminal UI: an auto-refreshing table of dev
// servers with arrow-key selection, kill (with confirmation), a detail view,
// and text filtering.
package tui

import (
	"fmt"
	"strings"
	"time"

	bkey "charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/table"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/inventory"
	"github.com/joshmcadams/whence/internal/kill"
	pm "github.com/joshmcadams/whence/internal/model"
	"github.com/joshmcadams/whence/internal/output"
)

const refreshInterval = 5 * time.Second

type mode int

const (
	modeList mode = iota
	modeConfirm
	modeDetail
	modeFilter
)

// styles.
var (
	titleStyle  = lipgloss.NewStyle().Bold(true)
	dimStyle    = lipgloss.NewStyle().Faint(true)
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	confirmBox  = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	detailLabel = lipgloss.NewStyle().Bold(true).Width(12)
)

// Model is the Bubble Tea model.
type Model struct {
	cfg config.Config

	raw  []pm.Server // full inventory
	rows []pm.Server // current filtered view (parallel to table rows)

	table table.Model
	ti    textinput.Model

	mode       mode
	all        bool
	query      string
	selected   pm.Server
	planTree   kill.Plan // blast radius whole-tree plan for the pending confirm
	planSingle kill.Plan // blast radius single-listener plan for the pending confirm
	killSingle bool      // confirm-time toggle: listener-only vs whole tree
	detailPlan kill.Plan // tree plan computed at detail-entry time (not clobbered by confirm)
	sortBy     string    // current sort key (port/uptime/name)
	theme      Theme
	keys       keyMap

	status string
	err    error

	width, height int

	loadSeq    int  // last sequence number issued to a load command
	appliedSeq int  // last sequence number applied to raw/rows
	loading    bool // true while a load command is in flight

	previewBoth func(pm.Server, kill.Opts) (kill.Plan, kill.Plan) // test seam
}

// New constructs the model.
func New(cfg config.Config, all bool) Model {
	t := table.New(
		table.WithColumns(columns(80)),
		table.WithFocused(true),
		table.WithHeight(15),
	)

	ti := textinput.New()
	ti.Placeholder = "filter by name, port, or description"
	ti.Prompt = "/"

	m := Model{
		cfg: cfg, all: all, table: t, ti: ti, theme: ThemeByName(cfg.Theme),
		keys:        defaultKeyMap(),
		previewBoth: kill.PreviewBoth,
		sortBy:      "port",
		// appliedSeq starts below any real sequence number (which starts at
		// 0) so the very first loadedMsg — issued by hand at seq 0 from
		// Init, see below — is never dropped as stale. loading starts true
		// to match: Init always fires an initial load, but Init can't
		// return the mutated Model (tea.Model.Init returns only a Cmd), so
		// there is no other place to record "a load is in flight" for that
		// first request.
		appliedSeq: -1,
		loading:    true,
	}
	m.applyTheme()
	return m
}

// applyTheme pushes the current theme's styles onto the table.
func (m *Model) applyTheme() {
	m.table.SetStyles(m.theme.tableStyles())
}

// cycleTheme advances to the next palette and applies it, updating the config in
// memory so it can be persisted.
func (m Model) cycleTheme() Model {
	m.theme = nextTheme(m.theme.Name)
	m.cfg.Theme = m.theme.Name
	m.applyTheme()
	m.status = "theme: " + m.theme.Name
	return m
}

// --- messages & commands ----------------------------------------------------

type loadedMsg struct {
	seq     int
	servers []pm.Server
	err     error
}
type killedMsg struct{ res kill.Result }
type tickMsg time.Time
type themeSavedMsg struct {
	name string
	err  error
}

// persistThemeCmd saves the (theme-updated) config to disk asynchronously.
func persistThemeCmd(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		_, err := config.Save(cfg)
		return themeSavedMsg{name: cfg.Theme, err: err}
	}
}

// loadCmd builds a load command stamped with seq; the caller is responsible
// for making sure seq matches whatever the model will expect when the
// resulting loadedMsg arrives (see nextLoadCmd and Init).
func loadCmd(cfg config.Config, seq int) tea.Cmd {
	return func() tea.Msg {
		s, err := inventory.Collect(cfg)
		return loadedMsg{seq: seq, servers: s, err: err}
	}
}

// nextLoadCmd issues a freshly-stamped load: it bumps the sequence counter,
// marks a load as in flight, and returns both the updated model and the
// command. Bubble Tea's Update passes/returns Model by value, so callers
// must use the returned model — exactly like the existing key/message
// handlers already do.
func (m Model) nextLoadCmd() (Model, tea.Cmd) {
	m.loadSeq++
	m.loading = true
	return m, loadCmd(m.cfg, m.loadSeq)
}

func killCmd(s pm.Server, opts kill.Opts) tea.Cmd {
	return func() tea.Msg {
		res := kill.Server(s, opts)
		return killedMsg{res: res}
	}
}

// killOpts builds the kill options from config and the confirm-time single toggle.
func (m Model) killOpts() kill.Opts {
	return kill.Opts{
		Timeout: time.Duration(m.cfg.KillTimeoutSeconds) * time.Second,
		Single:  m.killSingle,
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Init() tea.Cmd {
	// Stamped by hand at seq 0 to match New()'s untouched loadSeq (Init
	// cannot use nextLoadCmd: tea.Model.Init returns only a Cmd, so any
	// mutation to a Model copy inside it would be discarded — New() already
	// seeded loading=true to cover this first request).
	return tea.Batch(loadCmd(m.cfg, 0), tickCmd())
}

// --- update -----------------------------------------------------------------

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.table.SetColumns(columns(msg.Width))
		m.table.SetHeight(max(5, msg.Height-7))
		return m, nil

	case loadedMsg:
		if msg.seq <= m.appliedSeq {
			// A slower, older snapshot arriving after a newer one already
			// landed — applying it would roll the view back (e.g. a
			// just-killed server reappearing). Drop it.
			return m, nil
		}
		m.appliedSeq = msg.seq
		if msg.seq == m.loadSeq {
			// The newest-issued request just came back; no load remains
			// in flight. An older-but-still-newest-applied message (from
			// a request issued before the latest one) leaves loading true,
			// since the latest request is still outstanding.
			m.loading = false
		}
		m.err = msg.err
		m.raw = msg.servers
		m.rebuild()
		return m, nil

	case tickMsg:
		// Pause auto-refresh while typing a filter so the list doesn't jump,
		// and skip it entirely while a previous load is still in flight so
		// collects can't stack up unbounded.
		if m.mode == modeFilter || m.loading {
			return m, tickCmd()
		}
		nm, loadC := m.nextLoadCmd()
		return nm, tea.Batch(loadC, tickCmd())

	case killedMsg:
		if msg.res.Err != nil {
			m.status = errStyle.Render("✗ " + output.Describe(msg.res.Server) + " — " + output.Sanitize(msg.res.Err.Error()))
		} else {
			m.status = okStyle.Render("✓ killed " + output.Describe(msg.res.Server))
		}
		nm, loadC := m.nextLoadCmd()
		return nm, loadC // refresh immediately

	case themeSavedMsg:
		if msg.err != nil {
			m.status = errStyle.Render("theme: " + msg.name + " (not saved: " + msg.err.Error() + ")")
		} else {
			m.status = "theme: " + msg.name + " (saved)"
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// Default: forward to whichever child widget is active.
	return m.forward(msg)
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.mode {
	case modeFilter:
		if bkey.Matches(msg, m.keys.FilterCancel) {
			m.query = ""
			m.ti.SetValue("")
			m.ti.Blur()
			m.mode = modeList
			m.rebuild()
			return m, nil
		}
		if bkey.Matches(msg, m.keys.FilterApply) {
			m.ti.Blur()
			m.mode = modeList
			return m, nil
		}
		var cmd tea.Cmd
		m.ti, cmd = m.ti.Update(msg)
		m.query = m.ti.Value()
		m.rebuild()
		return m, cmd

	case modeConfirm:
		switch strings.ToLower(msg.String()) {
		case "y", "yes":
			opts := m.killOpts()
			m.mode = modeList
			m.status = "killing " + output.Describe(m.selected) + "…"
			return m, killCmd(m.selected, opts)
		case "s":
			// Toggle whole-tree vs listener-only (native processes only)
			// without re-previewing — both scopes were captured at x-press.
			plan := m.currentPlan()
			if !plan.Docker && !plan.NoPID {
				m.killSingle = !m.killSingle
			}
			return m, nil
		default: // n, esc, anything
			m.mode = modeList
			return m, nil
		}

	case modeDetail:
		if bkey.Matches(msg, m.keys.DetailBack) {
			m.mode = modeList
		}
		return m, nil
	}

	// modeList
	switch {
	case bkey.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case bkey.Matches(msg, m.keys.Refresh):
		m.status = ""
		nm, loadC := m.nextLoadCmd()
		return nm, loadC
	case bkey.Matches(msg, m.keys.All):
		m.all = !m.all
		m.rebuild()
		return m, nil
	case bkey.Matches(msg, m.keys.Theme):
		m = m.cycleTheme()
		return m, persistThemeCmd(m.cfg)
	case bkey.Matches(msg, m.keys.Sort):
		switch m.sortBy {
		case "port":
			m.sortBy = "uptime"
		case "uptime":
			m.sortBy = "name"
		default:
			m.sortBy = "port"
		}
		m.status = "sort: " + m.sortBy
		m.rebuild()
		return m, nil
	case bkey.Matches(msg, m.keys.Filter):
		m.mode = modeFilter
		m.ti.Focus()
		return m, textinput.Blink
	case bkey.Matches(msg, m.keys.Kill):
		if s, ok := m.current(); ok {
			m.selected = s
			m.killSingle = false
			m.planTree, m.planSingle = m.previewBoth(s, m.killOpts())
			m.mode = modeConfirm
		}
		return m, nil
	case bkey.Matches(msg, m.keys.Detail):
		if s, ok := m.current(); ok {
			m.selected = s
			tree, _ := m.previewBoth(s, m.killOpts())
			m.detailPlan = tree
			m.mode = modeDetail
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

func (m Model) forward(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.mode == modeFilter {
		m.ti, cmd = m.ti.Update(msg)
	} else {
		m.table, cmd = m.table.Update(msg)
	}
	return m, cmd
}

// current returns the server under the cursor.
func (m Model) current() (pm.Server, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.rows) {
		return pm.Server{}, false
	}
	return m.rows[i], true
}

// rebuild recomputes the filtered view and refreshes the table rows.
func (m *Model) rebuild() {
	m.rows = inventory.View(m.raw, m.cfg, m.all, 0, m.query)
	if m.sortBy != "" && m.sortBy != "port" {
		inventory.Sort(m.rows, m.sortBy)
	}
	descW := descWidth(m.width)
	rows := make([]table.Row, len(m.rows))
	for i, s := range m.rows {
		cells := output.Row(s, descW)
		rows[i] = table.Row{
			cells[0], // PORT
			cells[1], // PROTO
			cells[3], // UPTIME  (skip index 2 = PID)
			cells[4], // SRC
			cells[5], // SERVER
			cells[6], // DESCRIPTION
		}
	}
	m.table.SetRows(rows)
}

// currentPlan returns the confirm plan for the active scope (whole-tree or
// listener-only), keeping confirmView aware of the toggle state without
// storing a redundant field.
func (m Model) currentPlan() kill.Plan {
	if m.killSingle {
		return m.planSingle
	}
	return m.planTree
}

// --- view -------------------------------------------------------------------

func (m Model) View() tea.View {
	if m.mode == modeDetail {
		v := tea.NewView(m.detailView() + m.footerView())
		v.AltScreen = true
		return v
	}

	var b strings.Builder
	b.WriteString(m.headerView() + "\n")
	b.WriteString(m.table.View() + "\n")
	if len(m.rows) == 0 && len(m.raw) > 0 && !m.all && m.query == "" {
		hint := fmt.Sprintf("  0 of %d shown — press a to show all", len(m.raw))
		b.WriteString(dimStyle.Render(hint) + "\n")
	}

	switch m.mode {
	case modeFilter:
		b.WriteString(m.ti.View() + "\n")
	case modeConfirm:
		b.WriteString(m.confirmView() + "\n")
	}

	b.WriteString(m.footerView())
	v := tea.NewView(b.String())
	v.AltScreen = true
	return v
}

func (m Model) headerView() string {
	scope := "yours"
	if m.all {
		scope = "all"
	}
	title := m.theme.accentStyle().Render("whence")
	meta := dimStyle.Render(fmt.Sprintf("  %d shown · %s · %s", len(m.rows), scope, m.theme.Name))
	if m.query != "" {
		meta += dimStyle.Render(" · /" + m.query)
	}
	if m.sortBy != "" && m.sortBy != "port" {
		meta += dimStyle.Render(" · sort:" + m.sortBy)
	}
	if m.err != nil {
		meta += errStyle.Render("  scan error: " + output.Sanitize(m.err.Error()))
	}
	return title + meta
}

func (m Model) helpLine() string {
	var parts []string
	add := func(b bkey.Binding) {
		parts = append(parts, b.Help().Key+" "+b.Help().Desc)
	}

	switch m.mode {
	case modeList:
		add(m.keys.Up)
		add(m.keys.Kill)
		add(m.keys.Detail)
		add(m.keys.Filter)
		add(m.keys.All)
		add(m.keys.Theme)
		add(m.keys.Sort)
		add(m.keys.Refresh)
		add(m.keys.Quit)
	case modeConfirm:
		add(m.keys.ConfirmYes)
		plan := m.currentPlan()
		if !plan.Docker && !plan.NoPID {
			add(m.keys.ConfirmScope)
		}
		add(m.keys.ConfirmCancel)
	case modeDetail:
		add(m.keys.DetailBack)
	case modeFilter:
		add(m.keys.FilterApply)
		add(m.keys.FilterCancel)
	}

	return dimStyle.Render(strings.Join(parts, " · "))
}

func (m Model) footerView() string {
	help := m.helpLine()
	if m.status != "" {
		return m.status + "\n" + help
	}
	return help
}

// maxConfirmTreeLines caps how many tree rows are rendered so a large
// blast radius can't push the view off-screen; the header still states
// the true total.
const maxConfirmTreeLines = 12

// renderTreeLines returns a capped, sanitized list of tree-member lines for a
// kill plan, plus an overflow count. Shared by confirmView and detailView so
// neither can render a different blast radius from the other.
func renderTreeLines(p kill.Plan) (lines []string, overflow int) {
	all := p.Lines()
	shown := all
	if len(shown) > maxConfirmTreeLines {
		shown = shown[:maxConfirmTreeLines]
	}
	for _, line := range shown {
		lines = append(lines, output.Sanitize(line))
	}
	if len(all) > len(shown) {
		overflow = len(all) - len(shown)
	}
	return
}

// confirmView renders the kill confirmation: the target, the full process tree
// it will signal (the blast radius), the current scope, and the prompt. It uses
// the same kill.Plan the actual kill will act on, so it can't understate what
// dies — the safety property the CLI confirmation already has.
func (m Model) confirmView() string {
	p := m.currentPlan()
	var b strings.Builder

	head := "Kill " + output.Describe(m.selected)
	if !p.Docker && !p.NoPID && len(p.Tree) > 1 {
		head += fmt.Sprintf(" — %d processes", len(p.Tree))
	}
	b.WriteString(head + "\n")

	treeLines, overflow := renderTreeLines(p)
	for _, line := range treeLines {
		b.WriteString("  " + dimStyle.Render(line) + "\n")
	}
	if overflow > 0 {
		b.WriteString("  " + dimStyle.Render(fmt.Sprintf("… +%d more", overflow)) + "\n")
	}

	// Scope toggle is meaningful only for native process trees.
	if !p.Docker && !p.NoPID {
		scope := "whole tree"
		if m.killSingle {
			scope = "listener only"
		}
		b.WriteString(dimStyle.Render("scope: "+scope) + "\n")
	}
	b.WriteString("[y/N]")
	return confirmBox.Render(b.String())
}

func (m Model) detailView() string {
	s := m.selected
	row := func(label, val string) string {
		if val == "" {
			val = "-"
		}
		return detailLabel.Render(label) + val
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("whence — detail") + "\n\n")
	b.WriteString(row("Port", fmt.Sprintf("%d/%s", s.Port, output.Sanitize(s.Proto))) + "\n")
	b.WriteString(row("Bind", s.Exposure()) + "\n")
	b.WriteString(row("Server", output.Sanitize(s.DisplayName())) + "\n")
	b.WriteString(row("Source", output.SrcLabel(s.Source)) + "\n")
	if s.Source == pm.SourceDocker {
		b.WriteString(row("Container", output.Sanitize(s.Name)) + "\n")
		b.WriteString(row("Image", output.Sanitize(s.Cmdline)) + "\n")
	} else {
		b.WriteString(row("PID", fmt.Sprintf("%d (ppid %d)", s.PID, s.PPID)) + "\n")
		b.WriteString(row("Exe", output.Sanitize(s.Exe)) + "\n")
		b.WriteString(row("Command", output.Sanitize(s.Cmdline)) + "\n")
		b.WriteString(row("Cwd", output.Sanitize(s.Cwd)) + "\n")
	}
	b.WriteString(row("Uptime", output.HumanUptime(s.Uptime)) + "\n")
	b.WriteString(row("Confidence", fmt.Sprintf("%d", s.Confidence)) + "\n")
	if s.Project != nil {
		b.WriteString(row("Repo", output.Sanitize(s.Project.Root)) + "\n")
		b.WriteString(row("Marker", output.Sanitize(s.Project.Marker)) + "\n")
	}
	b.WriteString("\n" + detailLabel.Render("Description") + "\n")
	b.WriteString(wordWrap(output.Sanitize(s.Description()), 72) + "\n")

	treeLines, overflow := renderTreeLines(m.detailPlan)
	if len(treeLines) > 0 {
		b.WriteString("\n" + detailLabel.Render("Tree") + "\n")
		for _, line := range treeLines {
			b.WriteString(line + "\n")
		}
		if overflow > 0 {
			fmt.Fprintf(&b, "… +%d more\n", overflow)
		}
	}
	return b.String()
}

// --- helpers ----------------------------------------------------------------

func columns(width int) []table.Column {
	descW := descWidth(width)
	return []table.Column{
		{Title: "PORT", Width: 6},
		{Title: "PROTO", Width: 6},
		{Title: "UPTIME", Width: 8},
		{Title: "SRC", Width: 6},
		{Title: "SERVER", Width: 22},
		{Title: "DESCRIPTION", Width: descW},
	}
}

func descWidth(width int) int {
	// total minus the fixed columns and padding; clamp to a sane range.
	d := width - (6 + 6 + 8 + 6 + 22) - 14
	if d < 20 {
		return 20
	}
	if d > 80 {
		return 80
	}
	return d
}

func wordWrap(s string, width int) string {
	if s == "" {
		return "-"
	}
	words := strings.Fields(s)
	var b strings.Builder
	line := 0
	for i, w := range words {
		if line > 0 && line+1+len(w) > width {
			b.WriteString("\n")
			line = 0
		} else if i > 0 {
			b.WriteString(" ")
			line++
		}
		b.WriteString(w)
		line += len(w)
	}
	return b.String()
}
