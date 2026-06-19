package inventory

import (
	"testing"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/model"
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

func TestView_IgnorePorts(t *testing.T) {
	cfg := config.Config{ConfidenceThreshold: 50, IgnorePorts: []int{8080}}

	// Ignored even with all=true (where noise normally surfaces).
	got := View(servers(), cfg, true, 0, "")
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (8080 ignored)", len(got))
	}
	for _, s := range got {
		if s.Port == 8080 {
			t.Fatal("ignored port 8080 appeared even with --all")
		}
	}

	// An explicit --port is a direct request and overrides the ignore list.
	if got := View(servers(), cfg, true, 8080, ""); len(got) != 1 || got[0].Port != 8080 {
		t.Fatalf("explicit --port 8080 should override ignore, got %v", got)
	}
}

func TestView_IgnoreNames(t *testing.T) {
	// Case-insensitive match on the process name.
	cfg := config.Config{ConfidenceThreshold: 50, IgnoreNames: []string{"SSHD"}}
	got := View(servers(), cfg, true, 0, "")
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (sshd ignored)", len(got))
	}
	for _, s := range got {
		if s.Name == "sshd" {
			t.Fatal("ignored name sshd appeared")
		}
	}

	// Matches the project's display name too, so ignoring "jfdid" hides both
	// jfdid servers and leaves only sshd.
	cfg2 := config.Config{ConfidenceThreshold: 50, IgnoreNames: []string{"jfdid"}}
	if got := View(servers(), cfg2, true, 0, ""); len(got) != 1 || got[0].Name != "sshd" {
		t.Fatalf("ignoring 'jfdid' should leave only sshd, got %v", got)
	}
}
