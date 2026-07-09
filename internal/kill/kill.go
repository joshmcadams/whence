// Package kill terminates the process (or container) behind a listening port.
//
// For native processes it kills a process tree: it climbs from the listening
// PID through known launcher wrappers (npm, make, …) to the tree head — but
// never through a shell or init, so it won't take down your terminal — then
// signals the whole subtree SIGTERM and escalates to SIGKILL after a timeout.
// Docker-backed servers are stopped via `docker stop`.
package kill

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/joshmcadams/whence/internal/execx"
	"github.com/joshmcadams/whence/internal/model"
)

// Test seams: production code always uses these vars, tests swap them.
var (
	takeSnapshot         = snapshot
	terminatePID         = terminate
	forceKillPID         = forceKill
	pidAlive             = isAlive
	dockerCombinedOutput = execx.CombinedOutput
)

// Opts controls kill behavior.
type Opts struct {
	Timeout time.Duration // grace period before SIGKILL / docker stop -t
	Single  bool          // kill only the listening PID, not the tree
}

// Result reports the outcome of killing one server.
type Result struct {
	Server model.Server
	Killed bool
	Method string
	Err    error
}

// TreeMember is one process a native-process kill will signal.
type TreeMember struct {
	PID  int
	Name string
}

// Plan describes what killing a server will do, for preview/confirmation. For a
// native process Tree lists every process that will be signaled (the climbed
// root first, then its descendants); for a container Docker is true and Tree is
// nil. NoPID is set when a native server has no accessible pid.
type Plan struct {
	Server model.Server
	Docker bool
	NoPID  bool
	Tree   []TreeMember
}

// Preview computes, without killing anything, the process tree that Server would
// terminate. It takes a fresh snapshot, so it reflects the tree as it is right
// now; the eventual kill re-snapshots and may differ slightly if processes have
// since started or exited.
func Preview(s model.Server, o Opts) Plan {
	if s.Source == model.SourceDocker {
		return Plan{Server: s, Docker: true}
	}
	if s.PID <= 0 {
		return Plan{Server: s, NoPID: true}
	}
	return previewWith(s, o, takeSnapshot())
}

// PreviewBatch computes Plans for multiple servers with a single process-table
// snapshot, avoiding the N-snapshot cost when previewing a multi-port kill.
func PreviewBatch(servers []model.Server, o Opts) []Plan {
	tbl := takeSnapshot()
	plans := make([]Plan, len(servers))
	for i, s := range servers {
		if s.Source == model.SourceDocker {
			plans[i] = Plan{Server: s, Docker: true}
			continue
		}
		if s.PID <= 0 {
			plans[i] = Plan{Server: s, NoPID: true}
			continue
		}
		plans[i] = previewWith(s, o, tbl)
	}
	return plans
}

// previewWith computes a Plan from an already-taken process-table snapshot.
func previewWith(s model.Server, o Opts, tbl procTable) Plan {
	pids := planTree(s.PID, o.Single, tbl)
	tree := make([]TreeMember, len(pids))
	for i, p := range pids {
		tree[i] = TreeMember{PID: p, Name: tbl.name[p]}
	}
	return Plan{Server: s, Tree: tree}
}

// Lines renders what this Plan will do, for a confirmation prompt: a single
// action line for a container or a no-pid server, otherwise one line per process
// in the kill tree (the climbed root tagged when there's more than one). Shared
// by the CLI and TUI confirmations so neither can understate the blast radius.
func (p Plan) Lines() []string {
	switch {
	case p.Docker:
		return []string{"docker stop"}
	case p.NoPID:
		return []string{"no accessible pid (owned by another user; try elevated privileges)"}
	default:
		lines := make([]string, len(p.Tree))
		for i, m := range p.Tree {
			name := m.Name
			if name == "" {
				name = "?"
			}
			tag := ""
			if i == 0 && len(p.Tree) > 1 {
				tag = "  (tree root)"
			}
			lines[i] = fmt.Sprintf("%d %s%s", m.PID, name, tag)
		}
		return lines
	}
}

// launchers are wrapper processes we will climb through to find the tree head.
// Shells (bash/zsh/sh/fish/pwsh/cmd) are deliberately absent: climbing stops at
// them so an interactive session is never killed.
//
// Bare "node" is deliberately excluded too: it would let a kill climb up into a
// long-lived node host (e.g. an editor's extension host) and take it down. The
// common npm/yarn/pnpm chain is still handled — we climb through those wrappers,
// and any node helper subprocess is killed as a descendant of the subtree.
var launchers = map[string]bool{
	"npm": true, "npx": true, "yarn": true, "pnpm": true, "bun": true,
	"deno": true,
	"make": true, "cargo": true, "go": true, "air": true, "nodemon": true,
	"python": true, "python3": true, "ruby": true, "bundle": true,
	"foreman": true, "php": true, "rails": true,
	"gradle": true, "gradlew": true, "mvn": true, "dotnet": true,
	// Windows variants
	"npm.cmd": true, "yarn.cmd": true, "pnpm.cmd": true,
	"python.exe": true,
}

// Server kills the process or container behind a Server.
func Server(s model.Server, o Opts) Result {
	if s.Source == model.SourceDocker {
		return dockerStop(s, o)
	}
	if s.PID <= 0 {
		return Result{Server: s, Err: errors.New("no accessible pid (owned by another user; try elevated privileges)")}
	}
	method, err := killProcess(s.PID, o)
	return Result{Server: s, Killed: err == nil, Method: method, Err: err}
}

func killProcess(pid int, o Opts) (string, error) {
	tbl := takeSnapshot()

	method := "tree"
	if o.Single {
		method = "single"
	}
	tree := planTree(pid, o.Single, tbl)

	for _, p := range tree {
		_ = terminatePID(p) // best effort; some may already be gone
	}

	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if allDead(tree) {
			return method, nil
		}
		time.Sleep(100 * time.Millisecond)
	}

	var lastErr error
	for _, p := range tree {
		if pidAlive(p) {
			if err := forceKillPID(p); err != nil {
				lastErr = err
			}
		}
	}
	if !allDead(tree) {
		if lastErr == nil {
			lastErr = errors.New("processes survived SIGKILL")
		}
		return method, lastErr
	}
	return method, nil
}

func dockerStop(s model.Server, o Opts) Result {
	if s.Name == "" {
		return Result{Server: s, Err: errors.New("no container name")}
	}
	secs := int(o.Timeout.Seconds())
	if secs <= 0 {
		secs = 5
	}
	// `docker stop` waits up to `secs` for a graceful stop; bound the CLI call
	// itself beyond that so a wedged daemon can't hang the kill forever.
	timeout := time.Duration(secs)*time.Second + 10*time.Second
	if out, err := dockerCombinedOutput(timeout, "docker", "stop", "-t", strconv.Itoa(secs), s.Name); err != nil {
		return Result{Server: s, Method: "docker stop", Err: fmt.Errorf("%v: %s", err, out)}
	}
	return Result{Server: s, Killed: true, Method: "docker stop"}
}

// --- process table helpers --------------------------------------------------

type procTable struct {
	ppid     map[int]int
	name     map[int]string
	children map[int][]int
}

func snapshot() procTable {
	t := procTable{ppid: map[int]int{}, name: map[int]string{}, children: map[int][]int{}}
	procs, err := process.Processes()
	if err != nil {
		return t
	}
	for _, p := range procs {
		pid := int(p.Pid)
		ppid, err := p.Ppid()
		if err != nil {
			continue
		}
		t.ppid[pid] = int(ppid)
		t.children[int(ppid)] = append(t.children[int(ppid)], pid)
		if n, err := p.Name(); err == nil {
			t.name[pid] = n
		}
	}
	return t
}

// planTree resolves the set of pids a kill will signal: just the listening pid
// when single, otherwise the climbed tree head plus all its descendants. Shared
// by Preview and the actual kill so the two never disagree about scope.
func planTree(pid int, single bool, t procTable) []int {
	if single {
		return []int{pid}
	}
	return subtree(climb(pid, t), t)
}

// climb walks up through launcher wrappers to the tree head, stopping before
// any non-launcher (notably shells) and before init. seen guards against a
// ppid cycle (possible from mid-snapshot PID reuse, or stale ppids on
// Windows) turning this into an infinite loop.
func climb(pid int, t procTable) int {
	cur := pid
	seen := map[int]bool{cur: true}
	for {
		pp, ok := t.ppid[cur]
		if !ok || pp <= 1 {
			break
		}
		if !launchers[t.name[pp]] {
			break
		}
		if seen[pp] {
			break // cycle: pp already visited, stop climbing here
		}
		seen[pp] = true
		cur = pp
	}
	return cur
}

// subtree returns root plus all its descendants (BFS). seen guards against a
// cycle in the process table's child links so no pid is enqueued twice and
// the walk always terminates.
func subtree(root int, t procTable) []int {
	out := []int{root}
	seen := map[int]bool{root: true}
	queue := []int{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range t.children[cur] {
			if seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}

func allDead(pids []int) bool {
	for _, p := range pids {
		if pidAlive(p) {
			return false
		}
	}
	return true
}

func isAlive(pid int) bool {
	ok, err := process.PidExists(int32(pid))
	return err == nil && ok
}
