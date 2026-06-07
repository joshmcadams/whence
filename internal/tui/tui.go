// Package tui is the interactive terminal UI: an auto-refreshing table of dev
// servers with arrow-key selection, kill (with confirmation), a detail view,
// and text filtering.
package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/joshmcadams/ports/internal/config"
	"github.com/joshmcadams/ports/internal/inventory"
	"github.com/joshmcadams/ports/internal/kill"
	pm "github.com/joshmcadams/ports/internal/model"
	"github.com/joshmcadams/ports/internal/output"
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

	mode     mode
	all      bool
	query    string
	selected pm.Server

	status string
	err    error

	width, height int
}

// New constructs the model.
func New(cfg config.Config, all bool) Model {
	t := table.New(
		table.WithColumns(columns(80)),
		table.WithFocused(true),
		table.WithHeight(15),
	)
	st := table.DefaultStyles()
	st.Header = st.Header.Bold(true).BorderBottom(true)
	st.Selected = st.Selected.Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12"))
	t.SetStyles(st)

	ti := textinput.New()
	ti.Placeholder = "filter by name, port, or description"
	ti.Prompt = "/"

	return Model{cfg: cfg, all: all, table: t, ti: ti}
}

// --- messages & commands ----------------------------------------------------

type loadedMsg struct {
	servers []pm.Server
	err     error
}
type killedMsg struct{ res kill.Result }
type tickMsg time.Time

func loadCmd(cfg config.Config) tea.Cmd {
	return func() tea.Msg {
		s, err := inventory.Collect(cfg)
		return loadedMsg{servers: s, err: err}
	}
}

func killCmd(cfg config.Config, s pm.Server) tea.Cmd {
	return func() tea.Msg {
		res := kill.Server(s, kill.Opts{
			Timeout: time.Duration(cfg.KillTimeoutSeconds) * time.Second,
		})
		return killedMsg{res: res}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(loadCmd(m.cfg), tickCmd())
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
		m.err = msg.err
		m.raw = msg.servers
		m.rebuild()
		return m, nil

	case tickMsg:
		// Pause auto-refresh while typing a filter so the list doesn't jump.
		if m.mode == modeFilter {
			return m, tickCmd()
		}
		return m, tea.Batch(loadCmd(m.cfg), tickCmd())

	case killedMsg:
		if msg.res.Err != nil {
			m.status = errStyle.Render("✗ " + describe(msg.res.Server) + " — " + msg.res.Err.Error())
		} else {
			m.status = okStyle.Render("✓ killed " + describe(msg.res.Server))
		}
		return m, loadCmd(m.cfg) // refresh immediately

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Default: forward to whichever child widget is active.
	return m.forward(msg)
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}

	switch m.mode {
	case modeFilter:
		switch msg.String() {
		case "esc":
			m.query = ""
			m.ti.SetValue("")
			m.ti.Blur()
			m.mode = modeList
			m.rebuild()
			return m, nil
		case "enter":
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
			m.mode = modeList
			m.status = "killing " + describe(m.selected) + "…"
			return m, killCmd(m.cfg, m.selected)
		default: // n, esc, anything
			m.mode = modeList
			return m, nil
		}

	case modeDetail:
		switch msg.String() {
		case "esc", "enter", "q":
			m.mode = modeList
		}
		return m, nil
	}

	// modeList
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "r":
		m.status = ""
		return m, loadCmd(m.cfg)
	case "a":
		m.all = !m.all
		m.rebuild()
		return m, nil
	case "/":
		m.mode = modeFilter
		m.ti.Focus()
		return m, textinput.Blink
	case "x":
		if s, ok := m.current(); ok {
			m.selected = s
			m.mode = modeConfirm
		}
		return m, nil
	case "enter":
		if s, ok := m.current(); ok {
			m.selected = s
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
	descW := descWidth(m.width)
	rows := make([]table.Row, len(m.rows))
	for i, s := range m.rows {
		rows[i] = table.Row{
			fmt.Sprintf("%d", s.Port),
			s.Proto,
			output.HumanUptime(s.Uptime),
			output.SrcLabel(s.Source),
			s.DisplayName(),
			output.Truncate(s.Description(), descW),
		}
	}
	m.table.SetRows(rows)
}

// --- view -------------------------------------------------------------------

func (m Model) View() string {
	if m.mode == modeDetail {
		return m.detailView()
	}

	var b strings.Builder
	b.WriteString(m.headerView() + "\n")
	b.WriteString(m.table.View() + "\n")

	switch m.mode {
	case modeFilter:
		b.WriteString(m.ti.View() + "\n")
	case modeConfirm:
		prompt := fmt.Sprintf("Kill %s ?  [y/N]", describe(m.selected))
		b.WriteString(confirmBox.Render(prompt) + "\n")
	}

	b.WriteString(m.footerView())
	return b.String()
}

func (m Model) headerView() string {
	scope := "yours"
	if m.all {
		scope = "all"
	}
	title := titleStyle.Render("ports")
	meta := dimStyle.Render(fmt.Sprintf("  %d shown · %s", len(m.rows), scope))
	if m.query != "" {
		meta += dimStyle.Render(" · /" + m.query)
	}
	if m.err != nil {
		meta += errStyle.Render("  scan error: " + m.err.Error())
	}
	return title + meta
}

func (m Model) footerView() string {
	help := dimStyle.Render("↑/↓ move · x kill · enter details · / filter · a all · r refresh · q quit")
	if m.status != "" {
		return m.status + "\n" + help
	}
	return help
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
	b.WriteString(titleStyle.Render("ports — detail") + "\n\n")
	b.WriteString(row("Port", fmt.Sprintf("%d/%s", s.Port, s.Proto)) + "\n")
	b.WriteString(row("Server", s.DisplayName()) + "\n")
	b.WriteString(row("Source", output.SrcLabel(s.Source)) + "\n")
	if s.Source == pm.SourceDocker {
		b.WriteString(row("Container", s.Name) + "\n")
		b.WriteString(row("Image", s.Cmdline) + "\n")
	} else {
		b.WriteString(row("PID", fmt.Sprintf("%d (ppid %d)", s.PID, s.PPID)) + "\n")
		b.WriteString(row("Exe", s.Exe) + "\n")
		b.WriteString(row("Command", s.Cmdline) + "\n")
		b.WriteString(row("Cwd", s.Cwd) + "\n")
	}
	b.WriteString(row("Uptime", output.HumanUptime(s.Uptime)) + "\n")
	b.WriteString(row("Confidence", fmt.Sprintf("%d", s.Confidence)) + "\n")
	if s.Project != nil {
		b.WriteString(row("Repo", s.Project.Root) + "\n")
		b.WriteString(row("Marker", s.Project.Marker) + "\n")
	}
	b.WriteString("\n" + detailLabel.Render("Description") + "\n")
	b.WriteString(wordWrap(s.Description(), 72) + "\n")
	b.WriteString("\n" + dimStyle.Render("esc back · q list"))
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

func describe(s pm.Server) string {
	name := s.DisplayName()
	if name == "" {
		name = "(unknown)"
	}
	if s.Source == pm.SourceDocker {
		return fmt.Sprintf(":%d %s", s.Port, name)
	}
	return fmt.Sprintf(":%d %s (pid %d)", s.Port, name, s.PID)
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
