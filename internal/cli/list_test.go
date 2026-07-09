package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/model"
)

// Both cases below must error before any collection happens: they point
// config at an empty XDG_CONFIG_HOME temp dir (config.Load succeeds with
// defaults, no repo scan is needed to reach the error) and rely on
// runListWith returning immediately from validation.

func TestRunListWith_WatchAndJSONRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	err := runListWith(&listOpts{sortBy: "port", watch: true, asJSON: true, interval: 2 * time.Second}, "")
	if err == nil {
		t.Fatal("want error combining --watch and --json, got nil")
	}
	if !strings.Contains(err.Error(), "--json") {
		t.Errorf("err = %q, want it to mention --json", err.Error())
	}
}

func TestRunListWith_WatchIntervalBelowFloorRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	err := runListWith(&listOpts{sortBy: "port", watch: true, interval: 0}, "")
	if err == nil {
		t.Fatal("want error for interval below the 500ms floor, got nil")
	}
	if !strings.Contains(err.Error(), "500ms") {
		t.Errorf("err = %q, want it to mention 500ms", err.Error())
	}
}

func TestRunListWith_WatchNegativeIntervalRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	err := runListWith(&listOpts{sortBy: "port", watch: true, interval: -1 * time.Second}, "")
	if err == nil {
		t.Fatal("want error for a negative interval, got nil")
	}
	if !strings.Contains(err.Error(), "500ms") {
		t.Errorf("err = %q, want it to mention 500ms", err.Error())
	}
}

// --- listOnce characterization tests (collect seam) --------------------------

func saveCollectSeam(t *testing.T) {
	t.Helper()
	orig := collect
	t.Cleanup(func() { collect = orig })
}

func TestListOnce_HiddenCount(t *testing.T) {
	saveCollectSeam(t)
	collect = func(cfg config.Config) ([]model.Server, error) {
		return []model.Server{
			{Port: 5173, PID: 100, Source: model.SourceProcess, Confidence: 100},
			{Port: 9999, Source: model.SourceProcess, Confidence: 0},
			{Port: 8080, Source: model.SourceProcess, Confidence: 0},
		}, nil
	}

	// Mine only: threshold 50 → 1 of 3 shown.
	cfg := config.Config{ConfidenceThreshold: 50}
	servers, hidden, err := listOnce(cfg, &listOpts{sortBy: "port"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 1 {
		t.Errorf("servers = %d, want 1 (only the high-confidence one)", len(servers))
	}
	if hidden != 2 {
		t.Errorf("hidden = %d, want 2", hidden)
	}

	// All: threshold still 50, but all=true → 3 shown, 0 hidden.
	oAll := &listOpts{all: true, sortBy: "port"}
	servers, hidden, err = listOnce(cfg, oAll, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 3 {
		t.Errorf("servers = %d, want 3 (all=true)", len(servers))
	}
	if hidden != 0 {
		t.Errorf("hidden = %d, want 0", hidden)
	}
}

func TestListOnce_NoIgnore(t *testing.T) {
	saveCollectSeam(t)
	collect = func(cfg config.Config) ([]model.Server, error) {
		return []model.Server{
			{Port: 5432, PID: 100, Source: model.SourceProcess, Confidence: 100},
		}, nil
	}

	cfg := config.Config{
		ConfidenceThreshold: 50,
		IgnorePorts:         []int{5432},
	}

	// Default: 5432 is ignored → no servers.
	servers, _, err := listOnce(cfg, &listOpts{sortBy: "port"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 0 {
		t.Errorf("servers = %d, want 0 (port 5432 ignored)", len(servers))
	}

	// --no-ignore: bypasses ignore → 1 server.
	servers, _, err = listOnce(cfg, &listOpts{sortBy: "port", noIgnore: true}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 1 {
		t.Errorf("servers = %d, want 1 (--no-ignore bypassed the ignore list)", len(servers))
	}

	// The cfg value outside the call must still have its ignore list (value copy semantics).
	if len(cfg.IgnorePorts) != 1 || cfg.IgnorePorts[0] != 5432 {
		t.Errorf("cfg.IgnorePorts was mutated: %v", cfg.IgnorePorts)
	}
}

// --- query filtering (Feature 2) ---------------------------------------------

func TestListOnce_QueryFiltering(t *testing.T) {
	saveCollectSeam(t)
	collect = func(cfg config.Config) ([]model.Server, error) {
		return []model.Server{
			{Port: 5173, PID: 100, Source: model.SourceProcess, Confidence: 100,
				Project: &model.Project{Name: "jfdid", Description: "task system"}},
			{Port: 3000, PID: 200, Source: model.SourceProcess, Confidence: 100,
				Project: &model.Project{Name: "other", Description: "other app"}},
		}, nil
	}
	cfg := config.Config{ConfidenceThreshold: 50}

	// Empty query shows both.
	servers, _, err := listOnce(cfg, &listOpts{sortBy: "port"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 2 {
		t.Errorf("servers = %d, want 2 with empty query", len(servers))
	}

	// Query by name → one match.
	servers, _, err = listOnce(cfg, &listOpts{sortBy: "port"}, "jfdid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 1 || servers[0].Port != 5173 {
		t.Errorf("servers = %d, want 1 (jfdid on :5173)", len(servers))
	}

	// Query by description → one match.
	servers, _, err = listOnce(cfg, &listOpts{sortBy: "port"}, "task")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 1 || servers[0].Port != 5173 {
		t.Errorf("servers = %d, want 1 (task desc on :5173)", len(servers))
	}

	// Query --all with query: hidden count respects query.
	servers, hidden, err := listOnce(cfg, &listOpts{sortBy: "port"}, "jfdid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(servers) != 1 {
		t.Errorf("servers = %d, want 1", len(servers))
	}
	if hidden != 0 {
		t.Errorf("hidden = %d, want 0 (allView shares the same query)", hidden)
	}
}
