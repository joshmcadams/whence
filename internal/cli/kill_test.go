package cli

import (
	"testing"

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
