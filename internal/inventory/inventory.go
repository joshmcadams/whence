// Package inventory builds the merged list of dev servers (native + Docker) and
// provides the view filtering shared by the CLI and the TUI.
package inventory

import (
	"sort"
	"strconv"
	"strings"

	"github.com/joshmcadams/whence/internal/classify"
	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/docker"
	"github.com/joshmcadams/whence/internal/model"
	"github.com/joshmcadams/whence/internal/scan"
)

// Collect builds the full inventory: native process servers (scored) merged
// with Docker/compose servers. When a host port is published by a container,
// the container entry supersedes the native docker-proxy listener on that port.
func Collect(cfg config.Config) ([]model.Server, error) {
	// Start docker scan concurrently; it's independent of the native scan and
	// often slower (daemon round-trip), so overlapping the two saves wall time.
	type dockerResult struct{ servers []model.Server }
	dockerCh := make(chan dockerResult, 1)
	go func() {
		s, _ := docker.Servers() // best-effort; error deliberately ignored
		dockerCh <- dockerResult{s}
	}()

	procs, err := scan.Processes()
	if err != nil {
		return nil, err
	}
	classify.Process(procs, cfg)

	dockers := (<-dockerCh).servers

	return merge(dockers, procs), nil
}

// merge combines docker and native rows. A native row is suppressed only when
// it is docker's own listener for a published port: same port AND either
// (a) the row names the docker proxy machinery, (b) the row is unattributed
// (PID ≤ 0 — the root-owned proxy in an unprivileged scan), or (c) the
// container binding covers the native row's address (same exposure class,
// or the container is bound to all interfaces). A native listener on a
// genuinely different interface than the container survives.
func merge(dockers, procs []model.Server) []model.Server {
	byPort := make(map[int][]model.Server, len(dockers))
	for _, d := range dockers {
		byPort[d.Port] = append(byPort[d.Port], d)
	}

	merged := make([]model.Server, 0, len(procs)+len(dockers))
	merged = append(merged, dockers...)
	for _, p := range procs {
		suppressed := false
		for _, d := range byPort[p.Port] {
			if isDockerProxyName(p) || p.PID <= 0 || d.Exposure() == "all" || d.Exposure() == p.Exposure() {
				suppressed = true
				break
			}
		}
		if suppressed {
			continue
		}
		merged = append(merged, p)
	}
	return merged
}

// isDockerProxyName reports whether a server's name or executable basename
// identifies it as docker's own port-forwarding machinery rather than a
// genuine dev process: the Linux userland proxy, Docker Desktop's backend
// processes, or rootless Docker's port forwarder.
func isDockerProxyName(s model.Server) bool {
	exeBase := baseName(s.Exe)
	if s.Name == "docker-proxy" || exeBase == "docker-proxy" {
		return true
	}
	if s.Name == "rootlesskit" || exeBase == "rootlesskit" {
		return true
	}
	return strings.HasPrefix(s.Name, "com.docker.") || strings.HasPrefix(exeBase, "com.docker.")
}

// baseName returns the final path element of exe, or "" if exe is empty.
func baseName(exe string) string {
	if exe == "" {
		return ""
	}
	if i := strings.LastIndexAny(exe, `/\`); i >= 0 {
		return exe[i+1:]
	}
	return exe
}

// View applies the shared display filters: confidence (unless all), an optional
// exact port, and a free-text query over name/port/description. The result is a
// sorted copy; the input is not mutated.
func View(servers []model.Server, cfg config.Config, all bool, port int, query string) []model.Server {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]model.Server, 0, len(servers))
	for _, s := range servers {
		if port > 0 && s.Port != port {
			continue
		}
		// Ignore lists suppress entries even under --all — that's where system
		// noise (e.g. a root-owned Postgres) shows up. An explicit --port is a
		// direct request for that port and overrides the ignore list.
		if port == 0 && isIgnored(s, cfg) {
			continue
		}
		if !all && !classify.Mine(s, cfg) {
			continue
		}
		if q != "" && !matchesQuery(s, q) {
			continue
		}
		out = append(out, s)
	}
	Sort(out, "port")
	return out
}

// isIgnored reports whether a server is suppressed by the config ignore lists:
// its port is in IgnorePorts, or its process/container name or project name
// matches an entry in IgnoreNames (case-insensitive, exact).
func isIgnored(s model.Server, cfg config.Config) bool {
	for _, p := range cfg.IgnorePorts {
		if s.Port == p {
			return true
		}
	}
	if len(cfg.IgnoreNames) > 0 {
		name := strings.ToLower(s.Name)
		disp := strings.ToLower(s.DisplayName())
		for _, n := range cfg.IgnoreNames {
			n = strings.ToLower(strings.TrimSpace(n))
			if n == "" {
				continue
			}
			if name == n || disp == n {
				return true
			}
		}
	}
	return false
}

func matchesQuery(s model.Server, q string) bool {
	if strings.Contains(strings.ToLower(s.DisplayName()), q) {
		return true
	}
	if strings.Contains(strings.ToLower(s.Description()), q) {
		return true
	}
	return strings.Contains(strconv.Itoa(s.Port), q)
}

// Sort orders servers in place by the given key: port (default), uptime, name.
func Sort(s []model.Server, by string) {
	switch by {
	case "uptime":
		sort.Slice(s, func(i, j int) bool { return s[i].Uptime > s[j].Uptime })
	case "name":
		sort.Slice(s, func(i, j int) bool { return s[i].DisplayName() < s[j].DisplayName() })
	default:
		sort.Slice(s, func(i, j int) bool {
			if s[i].Port != s[j].Port {
				return s[i].Port < s[j].Port
			}
			return s[i].Proto < s[j].Proto
		})
	}
}
