package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/joshmcadams/whence/internal/config"
	pm "github.com/joshmcadams/whence/internal/model"
)

func testServers() []pm.Server {
	return []pm.Server{
		{Port: 5173, Proto: "tcp", PID: 100, Source: pm.SourceProcess, Confidence: 100,
			Cwd: "/r", Project: &pm.Project{Name: "jfdid", Description: "task system", Root: "/r"}},
		{Port: 9999, Proto: "tcp", Source: pm.SourceProcess, Confidence: 0},
	}
}

func newLoaded() Model {
	m := New(config.Config{ConfidenceThreshold: 50}, false)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = step(m, loadedMsg{servers: testServers()})
	return m
}

func step(m Model, msg tea.Msg) Model {
	nm, _ := m.Update(msg)
	return nm.(Model)
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestDefaultViewFiltersToMine(t *testing.T) {
	m := newLoaded()
	if len(m.rows) != 1 || m.rows[0].Port != 5173 {
		t.Fatalf("default view rows = %d, want 1 (the jfdid server)", len(m.rows))
	}
}

func TestToggleAll(t *testing.T) {
	m := step(newLoaded(), key("a"))
	if len(m.rows) != 2 {
		t.Fatalf("after 'a' rows = %d, want 2", len(m.rows))
	}
	m = step(m, key("a"))
	if len(m.rows) != 1 {
		t.Fatalf("after second 'a' rows = %d, want 1", len(m.rows))
	}
}

func TestDetailAndBack(t *testing.T) {
	m := step(newLoaded(), key("enter"))
	if m.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail", m.mode)
	}
	if m.selected.Port != 5173 {
		t.Errorf("selected port = %d, want 5173", m.selected.Port)
	}
	m = step(m, key("esc"))
	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList after esc", m.mode)
	}
}

func TestConfirmCancel(t *testing.T) {
	m := step(newLoaded(), key("x"))
	if m.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm after x", m.mode)
	}
	m = step(m, key("n"))
	if m.mode != modeList {
		t.Errorf("mode = %v, want modeList after n", m.mode)
	}
}

func TestConfirmPreviewsBlastRadius(t *testing.T) {
	m := step(newLoaded(), key("x"))
	if m.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm after x", m.mode)
	}
	// The confirm must be backed by the same plan the kill will act on, so it
	// can't understate what dies (the safety property the CLI already has).
	if len(m.plan.Tree) == 0 {
		t.Fatal("plan.Tree is empty — confirmation would hide the blast radius")
	}
	found := false
	for _, tm := range m.plan.Tree {
		if tm.PID == 100 { // the selected listener
			found = true
		}
	}
	if !found {
		t.Errorf("selected listener pid 100 not in previewed tree %+v", m.plan.Tree)
	}
	if !strings.Contains(m.View(), "100") {
		t.Error("confirm view does not render the pid; blast radius not shown to the user")
	}
}

func TestConfirmSingleToggle(t *testing.T) {
	m := step(newLoaded(), key("x"))
	if m.killSingle {
		t.Fatal("kill should default to the whole tree, not single")
	}
	m = step(m, key("s"))
	if !m.killSingle {
		t.Error("'s' in the confirm should toggle to listener-only")
	}
	if m.mode != modeConfirm || len(m.plan.Tree) == 0 {
		t.Errorf("after toggle: mode=%v treelen=%d, want still confirming with a tree", m.mode, len(m.plan.Tree))
	}
}

func TestCycleThemeUpdatesModelAndConfig(t *testing.T) {
	m := newLoaded() // default theme
	start := m.theme.Name
	// step() discards the returned cmd, so persistThemeCmd never runs — no disk write.
	m = step(m, key("t"))
	if m.theme.Name == start {
		t.Fatalf("theme did not change from %q", start)
	}
	if m.cfg.Theme != m.theme.Name {
		t.Errorf("cfg.Theme = %q, want %q (in sync for persistence)", m.cfg.Theme, m.theme.Name)
	}
}

func TestQuitKeys(t *testing.T) {
	for _, k := range []string{"q", "esc"} {
		m := newLoaded()
		_, cmd := m.Update(key(k))
		if cmd == nil {
			t.Errorf("key %q: expected quit cmd, got nil", k)
		}
	}
}

func TestEmptyViewShowsHint(t *testing.T) {
	// All servers below threshold → rows is empty but raw is not.
	m := New(config.Config{ConfidenceThreshold: 50}, false)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = step(m, loadedMsg{servers: []pm.Server{
		{Port: 9999, Proto: "tcp", Source: pm.SourceProcess, Confidence: 0},
	}})
	if len(m.rows) != 0 {
		t.Fatalf("expected 0 rows, got %d", len(m.rows))
	}
	if len(m.raw) == 0 {
		t.Fatal("raw should be non-empty")
	}
	v := m.View()
	if !strings.Contains(v, "press a") {
		t.Errorf("hint absent from view:\n%s", v)
	}
}

func TestHintAbsentWhenAll(t *testing.T) {
	// Same inventory, but all=true → nothing is hidden, hint must not appear.
	m := New(config.Config{ConfidenceThreshold: 50}, true)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = step(m, loadedMsg{servers: []pm.Server{
		{Port: 9999, Proto: "tcp", Source: pm.SourceProcess, Confidence: 0},
	}})
	v := m.View()
	if strings.Contains(v, "press a to show all") {
		t.Errorf("hint must not appear when all=true:\n%s", v)
	}
}

func TestHintAbsentWhenQueryFilters(t *testing.T) {
	// Query filters everything out — "press a" wouldn't help; hint must not appear.
	m := New(config.Config{ConfidenceThreshold: 50}, false)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = step(m, loadedMsg{servers: []pm.Server{
		{Port: 5173, Proto: "tcp", PID: 100, Source: pm.SourceProcess, Confidence: 100,
			Project: &pm.Project{Name: "myapp"}},
	}})
	m = step(m, key("/"))
	m = step(m, key("zzz")) // no match
	if m.query == "" {
		t.Fatal("query should be set")
	}
	v := m.View()
	if strings.Contains(v, "press a to show all") {
		t.Errorf("hint must not appear when query is active:\n%s", v)
	}
}

func TestFilterNarrows(t *testing.T) {
	m := step(newLoaded(), key("/"))
	if m.mode != modeFilter {
		t.Fatalf("mode = %v, want modeFilter", m.mode)
	}
	m = step(m, key("jfd"))
	if m.query != "jfd" || len(m.rows) != 1 {
		t.Errorf("query=%q rows=%d, want jfd/1", m.query, len(m.rows))
	}
	m = step(m, key("zzz")) // no match
	if len(m.rows) != 0 {
		t.Errorf("rows=%d after non-matching filter, want 0", len(m.rows))
	}
	m = step(m, key("esc"))
	if m.mode != modeList || m.query != "" {
		t.Errorf("esc should clear filter: mode=%v query=%q", m.mode, m.query)
	}
}
