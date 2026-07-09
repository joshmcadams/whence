package cli

import (
	"strings"
	"testing"
	"time"
)

// Both cases below must error before any collection happens: they point
// config at an empty XDG_CONFIG_HOME temp dir (config.Load succeeds with
// defaults, no repo scan is needed to reach the error) and rely on
// runListWith returning immediately from validation.

func TestRunListWith_WatchAndJSONRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	err := runListWith(&listOpts{sortBy: "port", watch: true, asJSON: true, interval: 2 * time.Second})
	if err == nil {
		t.Fatal("want error combining --watch and --json, got nil")
	}
	if !strings.Contains(err.Error(), "--json") {
		t.Errorf("err = %q, want it to mention --json", err.Error())
	}
}

func TestRunListWith_WatchIntervalBelowFloorRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	err := runListWith(&listOpts{sortBy: "port", watch: true, interval: 0})
	if err == nil {
		t.Fatal("want error for interval below the 500ms floor, got nil")
	}
	if !strings.Contains(err.Error(), "500ms") {
		t.Errorf("err = %q, want it to mention 500ms", err.Error())
	}
}

func TestRunListWith_WatchNegativeIntervalRejected(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	err := runListWith(&listOpts{sortBy: "port", watch: true, interval: -1 * time.Second})
	if err == nil {
		t.Fatal("want error for a negative interval, got nil")
	}
	if !strings.Contains(err.Error(), "500ms") {
		t.Errorf("err = %q, want it to mention 500ms", err.Error())
	}
}
