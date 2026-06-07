package inventory

import (
	"testing"

	"github.com/joshmcadams/ports/internal/config"
	"github.com/joshmcadams/ports/internal/model"
)

func servers() []model.Server {
	return []model.Server{
		{Port: 5173, Confidence: 100, Project: &model.Project{Name: "jfdid", Description: "task system"}},
		{Port: 8080, Confidence: 80, Project: &model.Project{Name: "jfdid"}},
		{Port: 9999, Confidence: 0, Name: "sshd"},
	}
}

func TestView_ConfidenceFilter(t *testing.T) {
	cfg := config.Config{ConfidenceThreshold: 50}
	mine := View(servers(), cfg, false, 0, "")
	if len(mine) != 2 {
		t.Fatalf("mine = %d, want 2", len(mine))
	}
	all := View(servers(), cfg, true, 0, "")
	if len(all) != 3 {
		t.Fatalf("all = %d, want 3", len(all))
	}
}

func TestView_PortAndQuery(t *testing.T) {
	cfg := config.Config{ConfidenceThreshold: 50}
	if got := View(servers(), cfg, true, 8080, ""); len(got) != 1 || got[0].Port != 8080 {
		t.Fatalf("port filter = %v", got)
	}
	if got := View(servers(), cfg, false, 0, "task"); len(got) != 1 || got[0].Port != 5173 {
		t.Fatalf("query 'task' = %v", got)
	}
	// query matches port number text
	if got := View(servers(), cfg, true, 0, "999"); len(got) != 1 || got[0].Port != 9999 {
		t.Fatalf("query '999' = %v", got)
	}
}
