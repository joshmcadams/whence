package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsUnderDevRoot(t *testing.T) {
	cfg := Config{DevRoots: []string{"/home/me/dev", "/work/projects"}}

	cases := []struct {
		name string
		dir  string
		want bool
	}{
		{"exact root", "/home/me/dev", true},
		{"child of root", "/home/me/dev/app", true},
		{"deep child", "/home/me/dev/app/cmd/x", true},
		{"second root", "/work/projects/thing", true},
		// The boundary the `root + separator` guard protects: a sibling dir whose
		// name merely starts with the root must NOT match.
		{"sibling sharing a prefix", "/home/me/devil", false},
		{"ancestor of root", "/home/me", false},
		{"unrelated", "/var/run", false},
		{"empty", "", false},
		// Matching is case-insensitive on every OS (see backlog/05): documents the
		// deliberate trade-off so it isn't "fixed" by accident.
		{"case-insensitive", "/home/me/DEV/app", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cfg.IsUnderDevRoot(tc.dir); got != tc.want {
				t.Errorf("IsUnderDevRoot(%q) = %v, want %v", tc.dir, got, tc.want)
			}
		})
	}
}

func TestLoad_MissingFileReturnsDefaults(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty dir → no config file
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load of a missing file should not error: %v", err)
	}
	def := Default()
	if cfg.ConfidenceThreshold != def.ConfidenceThreshold || cfg.Theme != def.Theme ||
		cfg.KillTimeoutSeconds != def.KillTimeoutSeconds {
		t.Errorf("missing file should yield defaults, got %+v", cfg)
	}
}

func TestLoad_OverlaysFileOntoDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	writeConfig(t, dir, "theme = \"amber\"\n")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "amber" {
		t.Errorf("theme = %q, want amber (from file)", cfg.Theme)
	}
	// A field absent from the file must keep its default, not reset to zero.
	if cfg.ConfidenceThreshold != Default().ConfidenceThreshold {
		t.Errorf("ConfidenceThreshold = %d, want default %d (file should overlay, not replace)",
			cfg.ConfidenceThreshold, Default().ConfidenceThreshold)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	want := Default()
	want.Theme = "teal"
	want.ConfidenceThreshold = 75
	want.KillTimeoutSeconds = 9
	want.IgnorePorts = []int{5432, 6379}

	if _, err := Save(want); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Theme != "teal" || got.ConfidenceThreshold != 75 || got.KillTimeoutSeconds != 9 {
		t.Errorf("round-trip scalars = %+v", got)
	}
	if len(got.IgnorePorts) != 2 || got.IgnorePorts[0] != 5432 || got.IgnorePorts[1] != 6379 {
		t.Errorf("round-trip IgnorePorts = %v, want [5432 6379]", got.IgnorePorts)
	}
}

func TestPath_UsesWhenceConfigFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := Path()
	// Portable across OSes: the file is always <base>/whence/config.toml.
	if filepath.Base(p) != "config.toml" || filepath.Base(filepath.Dir(p)) != "whence" {
		t.Errorf("Path() = %q, want .../whence/config.toml", p)
	}
}

// writeConfig writes config.toml under the XDG layout (<dir>/whence/config.toml).
func writeConfig(t *testing.T, dir, content string) {
	t.Helper()
	cfgDir := filepath.Join(dir, "whence")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.toml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
