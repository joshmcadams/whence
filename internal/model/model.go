// Package model holds the core data types shared across the scanner, the CLI,
// and (later) the TUI.
package model

import "time"

// Source identifies how a server was discovered.
type Source string

const (
	SourceProcess Source = "process" // a native host process holds the socket
	SourceDocker  Source = "docker"  // a container publishes the port (Phase 2)
)

// Server is a single listening port and everything we could learn about the
// process (or container) behind it.
type Server struct {
	Port      int           `json:"port"`
	Proto     string        `json:"proto"` // tcp | tcp6
	Address   string        `json:"address"`
	Source    Source        `json:"source"`
	PID       int           `json:"pid"`
	PPID      int           `json:"ppid"`
	Name      string        `json:"name"`    // process/short name
	Exe       string        `json:"exe"`     // executable path
	Cmdline   string        `json:"cmdline"` // full command line
	Cwd       string        `json:"cwd"`     // process working directory
	StartTime time.Time     `json:"startTime"`
	Uptime    time.Duration `json:"uptimeNs"`

	// Notes carries non-fatal scan diagnostics (e.g. "cwd: permission denied").
	Notes []string `json:"notes,omitempty"`

	// Project is the repo this server was launched from, if identified.
	Project *Project `json:"project,omitempty"`
	// Confidence (0–100) that this server is one the user is actively developing.
	Confidence int `json:"confidence"`
}

// DisplayName is the best short label for a server: its project name if known,
// otherwise the process/container name.
func (s Server) DisplayName() string {
	if s.Project != nil && s.Project.Name != "" {
		return s.Project.Name
	}
	return s.Name
}

// Description returns the project description if known, else empty.
func (s Server) Description() string {
	if s.Project != nil {
		return s.Project.Description
	}
	return ""
}

// Project is the repo a Server was launched from (populated in Phase 2).
type Project struct {
	Name        string `json:"name"`
	Root        string `json:"root"`
	Description string `json:"description,omitempty"`
	Marker      string `json:"marker,omitempty"`
}

// Attributed reports whether we managed to identify the owning process.
func (s Server) Attributed() bool { return s.PID > 0 }

// IsAllInterfaces reports whether a bind address means "every interface".
// "*" is how lsof (and therefore gopsutil on darwin) renders a wildcard bind.
func IsAllInterfaces(addr string) bool {
	switch addr {
	case "", "0.0.0.0", "::", "*":
		return true
	}
	return false
}

// IsLoopback reports whether a bind address is loopback-only.
func IsLoopback(addr string) bool {
	switch addr {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}

// Exposure classifies the bind address for display.
// "local" means loopback only; "all" means any interface (reachable off-box);
// anything else is the literal IP of a specific bound interface.
func (s Server) Exposure() string {
	switch {
	case IsLoopback(s.Address):
		return "local"
	case IsAllInterfaces(s.Address):
		return "all"
	default:
		return s.Address
	}
}
