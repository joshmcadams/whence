package cli

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/joshmcadams/whence/internal/kill"
	"github.com/joshmcadams/whence/internal/model"
)

func sample() []model.Server {
	return []model.Server{
		{Port: 5173, PID: 100, Source: model.SourceProcess, Project: &model.Project{Name: "jfdid"}},
		{Port: 5433, Source: model.SourceDocker, Name: "jfdid-db-1", Project: &model.Project{Name: "jfdid"}},
		{Port: 8080, Source: model.SourceDocker, Name: "jfdid-api-1", Project: &model.Project{Name: "jfdid"}},
		{Port: 3000, PID: 200, Source: model.SourceProcess, Name: "node", Project: &model.Project{Name: "other"}},
	}
}

func TestMatchTargets_ByPort(t *testing.T) {
	got, fuzzy := matchTargets(sample(), "5433")
	if len(got) != 1 || got[0].Port != 5433 {
		t.Fatalf("got %d matches, want 1 on :5433", len(got))
	}
	if fuzzy {
		t.Error("a port match should never be fuzzy")
	}
}

func TestMatchTargets_ByName(t *testing.T) {
	got, fuzzy := matchTargets(sample(), "JFDID") // case-insensitive, exact on project name
	if len(got) != 3 {
		t.Fatalf("got %d matches for jfdid, want 3", len(got))
	}
	if fuzzy {
		t.Error("exact project-name match should not be flagged fuzzy")
	}
}

func TestMatchTargets_NumericIsAlwaysPort(t *testing.T) {
	// "100" is a pid in the data but must be treated as a port, matching nothing.
	if got, _ := matchTargets(sample(), "100"); len(got) != 0 {
		t.Errorf("got %d matches, want 0 (100 is a port, none listen there)", len(got))
	}
}

func TestMatchTargets_ExactPreferredOverSubstring(t *testing.T) {
	servers := []model.Server{
		{Port: 3000, PID: 1, Source: model.SourceProcess, Project: &model.Project{Name: "api"}},
		{Port: 3001, PID: 2, Source: model.SourceProcess, Project: &model.Project{Name: "api-gateway"}},
	}
	got, fuzzy := matchTargets(servers, "api")
	if len(got) != 1 || got[0].Port != 3000 {
		t.Fatalf("kill api should hit only the exact 'api', got %d match(es)", len(got))
	}
	if fuzzy {
		t.Error("an exact match must not be flagged fuzzy")
	}
}

func TestMatchTargets_SubstringFallbackIsFuzzy(t *testing.T) {
	servers := []model.Server{
		{Port: 3000, PID: 1, Source: model.SourceProcess, Project: &model.Project{Name: "api"}},
		{Port: 3001, PID: 2, Source: model.SourceProcess, Project: &model.Project{Name: "api-gateway"}},
	}
	got, fuzzy := matchTargets(servers, "gate") // no exact match anywhere
	if len(got) != 1 || got[0].Port != 3001 {
		t.Fatalf("kill gate should fall back to substring 'api-gateway', got %d match(es)", len(got))
	}
	if !fuzzy {
		t.Error("a substring fallback must be flagged fuzzy so the confirmation can say so")
	}
}

func TestDedupeUnits(t *testing.T) {
	servers := []model.Server{
		{Port: 80, PID: 100, Source: model.SourceProcess},  // same pid, two ports
		{Port: 443, PID: 100, Source: model.SourceProcess}, // -> collapses to one
		{Port: 5433, Source: model.SourceDocker, Name: "db"},
		{Port: 5432, Source: model.SourceDocker, Name: "db"}, // same container -> one
	}
	got := dedupeUnits(servers)
	if len(got) != 2 {
		t.Fatalf("got %d units, want 2 (one pid, one container)", len(got))
	}
}

// --- runKillWith characterization tests --------------------------------------
//
// All fixtures below are docker-source so kill.PreviewBatch (called from
// confirmKill) short-circuits to a Docker plan for each unit instead of
// depending on real process-table content; only the (harmless, read-only)
// snapshot call itself still touches the live table.

// fakeKill returns a func(model.Server, kill.Opts) kill.Result that records
// every call and returns the canned per-name result (or a generic success if
// the name isn't in the map).
func fakeKill(results map[string]kill.Result) (func(model.Server, kill.Opts) kill.Result, func() []model.Server) {
	var mu sync.Mutex
	var calls []model.Server
	fn := func(s model.Server, _ kill.Opts) kill.Result {
		mu.Lock()
		calls = append(calls, s)
		mu.Unlock()
		if res, ok := results[s.Name]; ok {
			return res
		}
		return kill.Result{Server: s, Killed: true, Method: "docker stop"}
	}
	return fn, func() []model.Server {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
}

func TestRunKillWith_ForceSkipsConfirmation(t *testing.T) {
	var out, errOut bytes.Buffer
	killFn, calls := fakeKill(nil)
	d := killDeps{
		servers: []model.Server{{Port: 3000, Source: model.SourceDocker, Name: "web-1"}},
		kill:    killFn,
		in:      strings.NewReader(""),
		out:     &out,
		errOut:  &errOut,
	}
	if err := runKillWith("3000", &killOpts{force: true}, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls()) != 1 {
		t.Errorf("kill called %d time(s), want 1", len(calls()))
	}
	if !strings.Contains(out.String(), "✓ killed") {
		t.Errorf("output = %q, want a success line", out.String())
	}
	if strings.Contains(out.String(), "Proceed?") {
		t.Errorf("output = %q, want no confirmation prompt with --force", out.String())
	}
}

func TestRunKillWith_NonYesAnswerAborts(t *testing.T) {
	var out, errOut bytes.Buffer
	killFn, calls := fakeKill(nil)
	d := killDeps{
		servers: []model.Server{{Port: 3000, Source: model.SourceDocker, Name: "web-1"}},
		kill:    killFn,
		in:      strings.NewReader("n\n"),
		out:     &out,
		errOut:  &errOut,
	}
	err := runKillWith("3000", &killOpts{}, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls()) != 0 {
		t.Errorf("kill called %d time(s), want 0 (declined)", len(calls()))
	}
	if !strings.Contains(out.String(), "Proceed? [y/N]") {
		t.Errorf("output = %q, want the confirmation prompt", out.String())
	}
	if !strings.Contains(out.String(), "Aborted.") {
		t.Errorf("output = %q, want 'Aborted.'", out.String())
	}
}

func TestRunKillWith_EOFAborts(t *testing.T) {
	var out, errOut bytes.Buffer
	killFn, calls := fakeKill(nil)
	d := killDeps{
		servers: []model.Server{{Port: 3000, Source: model.SourceDocker, Name: "web-1"}},
		kill:    killFn,
		in:      strings.NewReader(""), // immediate EOF
		out:     &out,
		errOut:  &errOut,
	}
	err := runKillWith("3000", &killOpts{}, d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls()) != 0 {
		t.Errorf("kill called %d time(s), want 0 (EOF on prompt)", len(calls()))
	}
	if !strings.Contains(out.String(), "Aborted.") {
		t.Errorf("output = %q, want 'Aborted.'", out.String())
	}
}

func TestRunKillWith_YesProceedsForEachUnit(t *testing.T) {
	var out, errOut bytes.Buffer
	killFn, calls := fakeKill(nil)
	d := killDeps{
		servers: []model.Server{
			{Port: 3000, Source: model.SourceDocker, Name: "app-web-1", Project: &model.Project{Name: "app"}},
			{Port: 5432, Source: model.SourceDocker, Name: "app-db-1", Project: &model.Project{Name: "app"}},
		},
		kill:   killFn,
		in:     strings.NewReader("y\n"),
		out:    &out,
		errOut: &errOut,
	}
	if err := runKillWith("app", &killOpts{}, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls()) != 2 {
		t.Fatalf("kill called %d time(s), want 2 (one per deduped unit)", len(calls()))
	}
}

func TestRunKillWith_ExactMatchWording(t *testing.T) {
	var out, errOut bytes.Buffer
	killFn, _ := fakeKill(nil)
	d := killDeps{
		servers: []model.Server{{Port: 3000, Source: model.SourceDocker, Name: "web-1"}},
		kill:    killFn,
		in:      strings.NewReader("n\n"),
		out:     &out,
		errOut:  &errOut,
	}
	if err := runKillWith("web-1", &killOpts{}, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), `About to kill 1 target(s) matching "web-1"`) {
		t.Errorf("output = %q, want the exact-match wording", out.String())
	}
}

func TestRunKillWith_FuzzyMatchWording(t *testing.T) {
	var out, errOut bytes.Buffer
	killFn, _ := fakeKill(nil)
	d := killDeps{
		servers: []model.Server{{Port: 3000, Source: model.SourceDocker, Name: "web-1"}}, // "web" is a substring only
		kill:    killFn,
		in:      strings.NewReader("n\n"),
		out:     &out,
		errOut:  &errOut,
	}
	if err := runKillWith("web", &killOpts{}, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), `No exact match for "web"`) {
		t.Errorf("output = %q, want the fuzzy-match wording", out.String())
	}
}

func TestRunKillWith_FailureAggregation(t *testing.T) {
	var out, errOut bytes.Buffer
	killFn, _ := fakeKill(map[string]kill.Result{
		"app-db-1": {Killed: false, Err: assertErr("boom")},
	})
	d := killDeps{
		servers: []model.Server{
			{Port: 3000, Source: model.SourceDocker, Name: "app-web-1", Project: &model.Project{Name: "app"}},
			{Port: 5432, Source: model.SourceDocker, Name: "app-db-1", Project: &model.Project{Name: "app"}},
		},
		kill:   killFn,
		in:     strings.NewReader("y\n"),
		out:    &out,
		errOut: &errOut,
	}
	err := runKillWith("app", &killOpts{}, d)
	if err == nil || err.Error() != "1 of 2 kill(s) failed" {
		t.Errorf("err = %v, want %q", err, "1 of 2 kill(s) failed")
	}
	if strings.Count(out.String(), "✓ killed") != 1 {
		t.Errorf("output = %q, want exactly one ✓ line", out.String())
	}
	if strings.Count(out.String(), "✗") != 1 {
		t.Errorf("output = %q, want exactly one ✗ line", out.String())
	}
}

func TestRunKillWith_SingleMultiUnitWarning(t *testing.T) {
	var out, errOut bytes.Buffer
	killFn, _ := fakeKill(nil)
	d := killDeps{
		servers: []model.Server{
			{Port: 3000, Source: model.SourceDocker, Name: "app-web-1", Project: &model.Project{Name: "app"}},
			{Port: 5432, Source: model.SourceDocker, Name: "app-db-1", Project: &model.Project{Name: "app"}},
		},
		kill:   killFn,
		in:     strings.NewReader("n\n"),
		out:    &out,
		errOut: &errOut,
	}
	if err := runKillWith("app", &killOpts{single: true}, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(errOut.String(), "note: --single with 2 matched targets") {
		t.Errorf("errOut = %q, want the --single multi-unit warning", errOut.String())
	}
}

// assertErr is a tiny helper to build an error inline in table literals above.
type assertErr string

func (e assertErr) Error() string { return string(e) }

// TestRunKillWith_SanitizesEscapeSequences guards against a container/process
// name embedding a terminal escape (e.g. an ANSI cursor move) rewriting the
// confirmation prompt or status lines. describe/printPlan must neutralize it
// before it reaches d.out.
func TestRunKillWith_SanitizesEscapeSequences(t *testing.T) {
	var out, errOut bytes.Buffer
	killFn, _ := fakeKill(nil)
	d := killDeps{
		servers: []model.Server{{Port: 3000, Source: model.SourceDocker, Name: "web\x1b[1Ahack"}},
		kill:    killFn,
		in:      strings.NewReader("y\n"),
		out:     &out,
		errOut:  &errOut,
	}
	if err := runKillWith("3000", &killOpts{}, d); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.ContainsRune(out.String(), 0x1b) {
		t.Errorf("output contains raw ESC from server name:\n%q", out.String())
	}
	if !strings.Contains(out.String(), "web") || !strings.Contains(out.String(), "hack") {
		t.Errorf("sanitized output should still contain the harmless parts of the name: %q", out.String())
	}
}

func TestNewKillCmd_WiresFlags(t *testing.T) {
	cmd := newKillCmd()
	if cmd.Use != "kill <port|name>" {
		t.Errorf("Use = %q, want %q", cmd.Use, "kill <port|name>")
	}
	for _, name := range []string{"force", "single", "timeout"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing flag %q", name)
		}
	}
}

func TestDescribe(t *testing.T) {
	cases := []struct {
		name string
		s    model.Server
		want string
	}{
		{"docker", model.Server{Port: 3000, Source: model.SourceDocker, Name: "web-1"}, ":3000 web-1 [container web-1]"},
		{"process", model.Server{Port: 4000, Source: model.SourceProcess, PID: 42, Name: "node"}, ":4000 node [pid 42]"},
		{"unknown name", model.Server{Port: 5000, Source: model.SourceProcess, PID: 7}, ":5000 (unknown) [pid 7]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := describe(tc.s); got != tc.want {
				t.Errorf("describe = %q, want %q", got, tc.want)
			}
		})
	}
}
