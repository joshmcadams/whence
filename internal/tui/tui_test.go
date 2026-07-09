package tui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/kill"
	pm "github.com/joshmcadams/whence/internal/model"
)

func testServers() []pm.Server {
	return []pm.Server{
		{Port: 5173, Proto: "tcp", PID: 100, Source: pm.SourceProcess, Confidence: 100,
			Cwd: "/r", Project: &pm.Project{Name: "jfdid", Description: "task system", Root: "/r"}},
		{Port: 9999, Proto: "tcp", Source: pm.SourceProcess, Confidence: 0},
	}
}

// dockerTestServers returns a single docker-source server. kill.Preview
// short-circuits to a Docker plan for these without ever touching the real
// process table (kill.go: Preview returns early when Source == SourceDocker),
// so the confirm/kill-dispatch tests below stay hermetic.
func dockerTestServers() []pm.Server {
	return []pm.Server{
		{Port: 5432, Proto: "tcp", Source: pm.SourceDocker, Name: "db-1", Confidence: 100,
			Project: &pm.Project{Name: "app", Description: "database"}},
	}
}

func newLoadedDocker() Model {
	m := New(config.Config{ConfidenceThreshold: 50}, false)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = step(m, loadedMsg{servers: dockerTestServers()})
	return m
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
	m := newLoaded()
	m.previewBoth = func(s pm.Server, _ kill.Opts) (kill.Plan, kill.Plan) {
		return kill.Plan{Tree: []kill.TreeMember{{PID: 100, Name: "node"}}},
			kill.Plan{Tree: []kill.TreeMember{{PID: 100, Name: "node"}}}
	}
	m = step(m, key("x"))
	if m.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm after x", m.mode)
	}
	// The confirm must be backed by the same plan the kill will act on, so it
	// can't understate what dies (the safety property the CLI already has).
	if len(m.planTree.Tree) == 0 {
		t.Fatal("planTree.Tree is empty — confirmation would hide the blast radius")
	}
	found := false
	for _, tm := range m.planTree.Tree {
		if tm.PID == 100 { // the selected listener
			found = true
		}
	}
	if !found {
		t.Errorf("selected listener pid 100 not in previewed tree %+v", m.planTree.Tree)
	}
	if !strings.Contains(m.View(), "100") {
		t.Error("confirm view does not render the pid; blast radius not shown to the user")
	}
}

func TestConfirmSingleToggle(t *testing.T) {
	m := newLoaded()
	m.previewBoth = func(s pm.Server, _ kill.Opts) (kill.Plan, kill.Plan) {
		return kill.Plan{Tree: []kill.TreeMember{{PID: 1, Name: "root"}, {PID: 100, Name: "node"}}},
			kill.Plan{Tree: []kill.TreeMember{{PID: 100, Name: "node"}}}
	}
	m = step(m, key("x"))
	if m.killSingle {
		t.Fatal("kill should default to the whole tree, not single")
	}
	m = step(m, key("s"))
	if !m.killSingle {
		t.Error("'s' in the confirm should toggle to listener-only")
	}
	if m.mode != modeConfirm || len(m.currentPlan().Tree) == 0 {
		t.Errorf("after toggle: mode=%v treelen=%d, want still confirming with a tree", m.mode, len(m.currentPlan().Tree))
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

func TestDetailViewShowsBind(t *testing.T) {
	m := newLoaded() // row 0 is port 5173, Address=""  → Exposure()="all"
	m = step(m, key("enter"))
	if m.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail", m.mode)
	}
	v := m.View()
	if !strings.Contains(v, "Bind") {
		t.Errorf("detail view missing Bind field:\n%s", v)
	}
	if !strings.Contains(v, "all") && !strings.Contains(v, "local") {
		t.Errorf("detail view missing exposure value (all/local):\n%s", v)
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

// TestRebuildSanitizesRowContent guards against a server description or name
// embedding a terminal escape (from a repo's package.json / README / process
// name) rewriting the table. Note the full View() also contains lipgloss
// styling escapes, so this asserts on the rebuilt row cell strings, not the
// rendered View.
func TestRebuildSanitizesRowContent(t *testing.T) {
	m := New(config.Config{ConfidenceThreshold: 50}, false)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = step(m, loadedMsg{servers: []pm.Server{
		{Port: 5173, Proto: "tcp", PID: 100, Source: pm.SourceProcess, Confidence: 100,
			Project: &pm.Project{Name: "evil\x1b[8mname", Description: "desc\x1b]0;pwn\x07"}},
	}})
	for _, row := range m.table.Rows() {
		for _, cell := range row {
			if strings.ContainsRune(cell, 0x1b) {
				t.Errorf("row cell contains raw ESC: %q", cell)
			}
		}
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

func TestConfirmYesDispatchesKill(t *testing.T) {
	m := step(newLoadedDocker(), key("x"))
	if m.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm after x", m.mode)
	}
	nm, cmd := m.Update(key("y"))
	m2 := nm.(Model)
	if m2.mode != modeList {
		t.Errorf("mode = %v, want modeList after y", m2.mode)
	}
	if !strings.Contains(m2.status, "killing") {
		t.Errorf("status = %q, want it to mention 'killing'", m2.status)
	}
	if cmd == nil {
		t.Error("expected a non-nil kill command (not executed here)")
	}
}

func TestKilledMsgSuccessSetsStatus(t *testing.T) {
	m := newLoadedDocker()
	s := dockerTestServers()[0]
	nm, cmd := m.Update(killedMsg{res: kill.Result{Server: s}})
	m2 := nm.(Model)
	if !strings.Contains(m2.status, "✓ killed") {
		t.Errorf("status = %q, want it to contain '✓ killed'", m2.status)
	}
	if cmd == nil {
		t.Error("expected a non-nil reload command")
	}
}

func TestKilledMsgErrorSetsStatus(t *testing.T) {
	m := newLoadedDocker()
	s := dockerTestServers()[0]
	nm, _ := m.Update(killedMsg{res: kill.Result{Server: s, Err: errors.New("nope")}})
	m2 := nm.(Model)
	if !strings.Contains(m2.status, "✗") {
		t.Errorf("status = %q, want it to contain '✗'", m2.status)
	}
	if !strings.Contains(m2.status, "nope") {
		t.Errorf("status = %q, want it to contain the error message 'nope'", m2.status)
	}
}

// TestLoadedMsgDropsStaleSnapshot guards the refresh-integrity invariant: a
// slower, older collect landing after a faster, newer one must not roll the
// view back (e.g. a just-killed server reappearing).
func TestLoadedMsgDropsStaleSnapshot(t *testing.T) {
	m := New(config.Config{ConfidenceThreshold: 50}, true) // all=true: nothing filtered out
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 24})

	m, _ = m.nextLoadCmd() // seq 1
	m, _ = m.nextLoadCmd() // seq 2

	a := pm.Server{Port: 1111, Proto: "tcp", Source: pm.SourceProcess, Confidence: 100}
	b := pm.Server{Port: 2222, Proto: "tcp", Source: pm.SourceProcess, Confidence: 100}
	c := pm.Server{Port: 3333, Proto: "tcp", Source: pm.SourceProcess, Confidence: 100}

	m = step(m, loadedMsg{seq: 2, servers: []pm.Server{a, b}}) // fast, newer collect lands first
	m = step(m, loadedMsg{seq: 1, servers: []pm.Server{c}})    // slow, older collect lands late

	if len(m.rows) != 2 {
		t.Fatalf("rows = %d, want 2 (the seq=2 snapshot); stale seq=1 must not have applied", len(m.rows))
	}
	ports := map[int]bool{}
	for _, r := range m.rows {
		ports[r.Port] = true
	}
	if !ports[1111] || !ports[2222] {
		t.Errorf("rows = %+v, want ports 1111 and 2222 from the seq=2 snapshot", m.rows)
	}
}

// TestLoadedMsgKeepsLoadingWhileNewerRequestOutstanding pins the exact
// semantics of Step 1: an older-but-newest-applied message is still applied
// (it's the freshest data seen so far), but must NOT clear m.loading, since
// a newer request (the one that will supersede it) is still in flight.
func TestLoadedMsgKeepsLoadingWhileNewerRequestOutstanding(t *testing.T) {
	m := New(config.Config{ConfidenceThreshold: 50}, true)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 24})

	m, _ = m.nextLoadCmd() // seq 1
	m, _ = m.nextLoadCmd() // seq 2 — the newest-issued request, still outstanding

	a := pm.Server{Port: 1111, Proto: "tcp", Source: pm.SourceProcess, Confidence: 100}
	m = step(m, loadedMsg{seq: 1, servers: []pm.Server{a}}) // the older request returns first

	if !m.loading {
		t.Error("m.loading = false, want true: seq=2 is still outstanding")
	}
	if len(m.rows) != 1 || m.rows[0].Port != 1111 {
		t.Errorf("rows = %+v, want the seq=1 snapshot applied (it's newer than anything applied so far)", m.rows)
	}
}

// TestTickSkipsLoadWhileLoading guards against unbounded collect stacking:
// a tick that fires while a previous load is still in flight must not issue
// another one.
func TestTickSkipsLoadWhileLoading(t *testing.T) {
	m := newLoaded()
	m.loading = true
	before := m.loadSeq
	m = step(m, tickMsg(time.Now()))
	if m.loadSeq != before {
		t.Errorf("loadSeq = %d, want unchanged at %d (tick must not load while m.loading)", m.loadSeq, before)
	}
}

// TestTickLoadsWhenIdle is the counterpart: once no load is in flight, the
// tick must resume issuing loads.
func TestTickLoadsWhenIdle(t *testing.T) {
	m := newLoaded()
	if m.loading {
		t.Fatal("precondition: m.loading should be false after the initial load applied")
	}
	before := m.loadSeq
	m = step(m, tickMsg(time.Now()))
	if m.loadSeq != before+1 {
		t.Errorf("loadSeq = %d, want %d (tick should issue a load while idle)", m.loadSeq, before+1)
	}
}

// TestManualRefreshAlwaysLoads ensures 'r' loads even while an
// auto-refresh is in flight — user intent wins over the tick guard.
func TestManualRefreshAlwaysLoads(t *testing.T) {
	m := newLoaded()
	m.loading = true
	before := m.loadSeq
	m = step(m, key("r"))
	if m.loadSeq != before+1 {
		t.Errorf("loadSeq = %d, want %d ('r' must load even while m.loading)", m.loadSeq, before+1)
	}
}

// --- PreviewBoth seam tests -------------------------------------------------

func TestConfirmUsesPreviewBothSeam(t *testing.T) {
	m := newLoaded()
	var capturedPM pm.Server
	var calls int
	m.previewBoth = func(s pm.Server, _ kill.Opts) (kill.Plan, kill.Plan) {
		capturedPM = s
		calls++
		return kill.Plan{Tree: []kill.TreeMember{{PID: 1, Name: "root"}, {PID: 2, Name: "make"}, {PID: 3, Name: "node"}}},
			kill.Plan{Tree: []kill.TreeMember{{PID: 3, Name: "node"}}}
	}
	m = step(m, key("x"))

	if m.mode != modeConfirm {
		t.Fatalf("mode = %v, want modeConfirm", m.mode)
	}
	if capturedPM.Port != 5173 {
		t.Errorf("previewBoth called with %+v, want server on 5173", capturedPM)
	}
	if calls != 1 {
		t.Errorf("previewBoth called %d times, want exactly 1", calls)
	}
	// Default scope: whole tree
	if len(m.planTree.Tree) != 3 || len(m.planSingle.Tree) != 1 {
		t.Fatalf("planTree=%d planSingle=%d, want 3 and 1", len(m.planTree.Tree), len(m.planSingle.Tree))
	}
	if m.killSingle {
		t.Error("killSingle should default to false (whole tree)")
	}

	// Toggle scope with 's' — must re-render without calling previewBoth again.
	beforeView := m.View()
	m = step(m, key("s"))
	if !m.killSingle {
		t.Error("killSingle should be true after 's' toggle")
	}
	if calls != 1 {
		t.Errorf("previewBoth called %d times after toggle, want still 1 (no re-snapshot)", calls)
	}
	afterView := m.View()
	// Whole-tree view must contain all three PIDs; single-tree only the listener.
	if !strings.Contains(beforeView, "1") || !strings.Contains(beforeView, "2") || !strings.Contains(beforeView, "3") {
		t.Errorf("whole-tree view should show all pids:\n%s", beforeView)
	}
	if !strings.Contains(afterView, "listener only") {
		t.Errorf("single-tree view should show 'listener only':\n%s", afterView)
	}

	// Toggle back — still 1 call.
	m = step(m, key("s"))
	if m.killSingle {
		t.Error("killSingle should be false after toggling back")
	}
	if calls != 1 {
		t.Errorf("previewBoth called %d times after second toggle, want still 1", calls)
	}
}

func TestConfirmPreviewBothDockerShortCircuits(t *testing.T) {
	m := newLoadedDocker()
	var calls int
	m.previewBoth = func(s pm.Server, _ kill.Opts) (kill.Plan, kill.Plan) {
		calls++
		return kill.Plan{Server: s, Docker: true}, kill.Plan{Server: s, Docker: true}
	}
	m = step(m, key("x"))

	if calls != 1 {
		t.Errorf("previewBoth called %d times, want 1", calls)
	}
	if !m.planTree.Docker || !m.planSingle.Docker {
		t.Error("both plans should be docker plans")
	}

	// Scoping is not togglable for docker, so planTree == planSingle
	// and confirmView should not show the scope toggle.
	v := m.View()
	if strings.Contains(v, "s to toggle") {
		t.Error("docker confirm should not offer scope toggle")
	}
}

func TestFooterHelp_ListMode(t *testing.T) {
	m := newLoaded()
	v := m.View()
	for _, want := range []string{"move", "kill", "details", "filter", "all", "theme", "refresh", "quit"} {
		if !strings.Contains(v, want) {
			t.Errorf("list-mode footer missing %q", want)
		}
	}
}

func TestFooterHelp_ConfirmMode(t *testing.T) {
	m := newLoaded()
	m.previewBoth = func(s pm.Server, _ kill.Opts) (kill.Plan, kill.Plan) {
		return kill.Plan{Tree: []kill.TreeMember{{PID: 100, Name: "node"}}},
			kill.Plan{Tree: []kill.TreeMember{{PID: 100, Name: "node"}}}
	}
	m = step(m, key("x"))
	v := m.View()
	if !strings.Contains(v, "confirm") {
		t.Error("confirm footer missing 'confirm'")
	}
	if !strings.Contains(v, "toggle scope") {
		t.Error("confirm footer missing 'toggle scope'")
	}
	if !strings.Contains(v, "cancel") {
		t.Error("confirm footer missing 'cancel'")
	}
}

func TestFooterHelp_ConfirmScopeAbsentForDocker(t *testing.T) {
	m := newLoadedDocker()
	m = step(m, key("x"))
	v := m.View()
	if strings.Contains(v, "toggle scope") {
		t.Error("docker confirm must not show toggle scope")
	}
}

func TestFooterHelp_DetailMode(t *testing.T) {
	m := newLoaded()
	m = step(m, key("enter"))
	v := m.View()
	if !strings.Contains(v, "back") {
		t.Error("detail footer missing 'back'")
	}
}

func TestFooterHelp_FilterMode(t *testing.T) {
	m := newLoaded()
	m = step(m, key("/"))
	v := m.View()
	if !strings.Contains(v, "apply") {
		t.Error("filter footer missing 'apply'")
	}
	if !strings.Contains(v, "clear") {
		t.Error("filter footer missing 'clear'")
	}
}

// --- Feature 1: detail view process tree -------------------------------------

func TestDetailViewShowsProcessTree(t *testing.T) {
	m := newLoaded()
	m.previewBoth = func(s pm.Server, _ kill.Opts) (kill.Plan, kill.Plan) {
		return kill.Plan{Tree: []kill.TreeMember{
			{PID: 1, Name: "bash"}, {PID: 100, Name: "node"}, {PID: 101, Name: "vite"},
		}}, kill.Plan{Tree: []kill.TreeMember{{PID: 100, Name: "node"}}}
	}
	m = step(m, key("enter"))
	if m.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail", m.mode)
	}
	if len(m.detailPlan.Tree) == 0 {
		t.Fatal("detailPlan.Tree is empty — detail view would hide the tree")
	}
	v := m.View()
	for _, pid := range []string{"1", "100", "101"} {
		if !strings.Contains(v, pid) {
			t.Errorf("detail view missing pid %s:\n%s", pid, v)
		}
	}
	if !strings.Contains(v, "Tree") {
		t.Errorf("detail view missing Tree section:\n%s", v)
	}
}

func TestDetailViewDockerTree(t *testing.T) {
	m := newLoadedDocker()
	m.previewBoth = func(s pm.Server, _ kill.Opts) (kill.Plan, kill.Plan) {
		return kill.Plan{Server: s, Docker: true}, kill.Plan{Server: s, Docker: true}
	}
	m = step(m, key("enter"))
	if m.mode != modeDetail {
		t.Fatalf("mode = %v, want modeDetail", m.mode)
	}
	v := m.View()
	if !strings.Contains(v, "docker stop") {
		t.Errorf("detail view missing 'docker stop' for docker server:\n%s", v)
	}
}

// --- Feature 3: sort ---------------------------------------------------------

func sortTestServers() []pm.Server {
	return []pm.Server{
		{Port: 3000, Proto: "tcp", PID: 200, Source: pm.SourceProcess, Confidence: 100,
			Cwd: "/r", Name: "alpha", Project: &pm.Project{Name: "alpha", Root: "/r"}},
		{Port: 5173, Proto: "tcp", PID: 100, Source: pm.SourceProcess, Confidence: 100,
			Cwd: "/r", Name: "beta", Project: &pm.Project{Name: "beta", Root: "/r"}},
		{Port: 8080, Proto: "tcp", PID: 300, Source: pm.SourceProcess, Confidence: 100,
			Cwd: "/r", Name: "gamma", Project: &pm.Project{Name: "gamma", Root: "/r"}},
	}
}

func newLoadedSort() Model {
	m := New(config.Config{ConfidenceThreshold: 50}, true)
	m = step(m, tea.WindowSizeMsg{Width: 100, Height: 24})
	m = step(m, loadedMsg{servers: sortTestServers()})
	return m
}

func TestSortCycle(t *testing.T) {
	m := newLoadedSort()
	if m.sortBy != "port" {
		t.Fatalf("default sortBy = %q, want port", m.sortBy)
	}
	firstPort := m.rows[0].Port
	if firstPort != 3000 {
		t.Fatalf("default sort should be by port ascending, got first port %d", firstPort)
	}

	// Press s: port → uptime
	m = step(m, key("s"))
	if m.sortBy != "uptime" {
		t.Errorf("sortBy after first s = %q, want uptime", m.sortBy)
	}
	if !strings.Contains(m.status, "sort: uptime") {
		t.Errorf("status after sort = %q, want it to mention 'sort: uptime'", m.status)
	}

	// Press s: uptime → name
	m = step(m, key("s"))
	if m.sortBy != "name" {
		t.Errorf("sortBy after second s = %q, want name", m.sortBy)
	}
	if m.rows[0].Name != "alpha" {
		t.Errorf("sort by name: first row = %q, want alpha", m.rows[0].Name)
	}

	// Press s: name → port (wrap)
	m = step(m, key("s"))
	if m.sortBy != "port" {
		t.Errorf("sortBy after third s = %q, want port (wrap)", m.sortBy)
	}

	// Header should not show sort for default
	v := m.View()
	if strings.Contains(v, "sort:port") {
		t.Error("header should not show sort:port (default)")
	}
}

func TestSortHeaderVisibility(t *testing.T) {
	m := newLoadedSort()
	m = step(m, key("s")) // port → uptime
	v := m.View()
	if !strings.Contains(v, "sort:uptime") {
		t.Errorf("header missing sort indicator:\n%s", v)
	}
}

func TestSortInFooterHelp(t *testing.T) {
	m := newLoadedSort()
	v := m.View()
	if !strings.Contains(v, "sort") {
		t.Errorf("list-mode footer missing 'sort':\n%s", v)
	}
}
