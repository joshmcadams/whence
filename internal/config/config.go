// Package config loads and persists user configuration: the dev roots used to
// decide which servers are "yours", plus ignore lists and kill behavior.
package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
)

// Config is the on-disk configuration. Fields beyond DevRoots are scaffolded
// now and consumed in later phases.
type Config struct {
	// DevRoots are directories under which a process's cwd marks it as "yours".
	DevRoots []string `toml:"dev_roots"`
	// IgnorePorts are never shown even with --all.
	IgnorePorts []int `toml:"ignore_ports"`
	// IgnoreNames are process names to suppress.
	IgnoreNames []string `toml:"ignore_names"`
	// KillTimeoutSeconds is the grace period before SIGKILL (Phase 3).
	KillTimeoutSeconds int `toml:"kill_timeout_seconds"`
	// ConfidenceThreshold is the minimum score to be shown without --all (Phase 2).
	ConfidenceThreshold int `toml:"confidence_threshold"`
	// Theme is the TUI color palette name; unknown values fall back to the default.
	Theme string `toml:"theme"`
}

// Default returns the built-in configuration. Dev roots cover the common
// conventions across macOS/Linux/WSL; matching is case-insensitive (see
// IsUnderDevRoot) so ~/Development and ~/development both work.
func Default() Config {
	home, _ := os.UserHomeDir()
	join := func(p string) string { return filepath.Join(home, p) }
	return Config{
		DevRoots: []string{
			join("development"),
			join("dev"),
			join("Projects"),
			join("projects"),
			join("src"),
			join("code"),
			join("Code"),
			join("work"),
			join("go/src"),
		},
		IgnorePorts:         nil,
		IgnoreNames:         nil,
		KillTimeoutSeconds:  5,
		ConfidenceThreshold: 50,
		Theme:               "indigo",
	}
}

// Path returns the config file location (XDG on unix, %AppData% on Windows).
func Path() string {
	if runtime.GOOS == "windows" {
		if base := os.Getenv("AppData"); base != "" {
			return filepath.Join(base, "whence", "config.toml")
		}
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "whence", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "whence", "config.toml")
}

// Load reads the config file, falling back to defaults for any missing fields.
// A missing file is not an error: defaults are returned.
func Load() (Config, error) {
	cfg := Default()
	p := Path()
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	// Overlay file values onto defaults.
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// Save writes cfg to the config path, creating the parent directory. It returns
// the path written.
func Save(cfg Config) (string, error) {
	p := Path()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return p, err
	}
	f, err := os.Create(p)
	if err != nil {
		return p, err
	}
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		_ = f.Close()
		return p, err
	}
	return p, f.Close()
}

// IsUnderDevRoot reports whether dir sits under any configured dev root.
// Comparison is case-insensitive to tolerate Development vs development and
// case-insensitive filesystems (macOS, Windows).
func (c Config) IsUnderDevRoot(dir string) bool {
	if dir == "" {
		return false
	}
	d := normalize(dir)
	for _, root := range c.DevRoots {
		r := normalize(root)
		if d == r || strings.HasPrefix(d, r+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func normalize(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	abs = filepath.Clean(abs)
	return strings.ToLower(abs)
}
