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
	"os/exec"
	"strconv"
	"time"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/jmcadams/ports/internal/model"
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
	tbl := snapshot()

	root := pid
	method := "single"
	var tree []int
	if o.Single {
		tree = []int{pid}
	} else {
		root = climb(pid, tbl)
		tree = subtree(root, tbl)
		method = "tree"
	}

	for _, p := range tree {
		_ = terminate(p) // best effort; some may already be gone
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
		if isAlive(p) {
			if err := forceKill(p); err != nil {
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
	cmd := exec.Command("docker", "stop", "-t", strconv.Itoa(secs), s.Name)
	if out, err := cmd.CombinedOutput(); err != nil {
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

// climb walks up through launcher wrappers to the tree head, stopping before
// any non-launcher (notably shells) and before init.
func climb(pid int, t procTable) int {
	cur := pid
	for {
		pp, ok := t.ppid[cur]
		if !ok || pp <= 1 {
			break
		}
		if !launchers[t.name[pp]] {
			break
		}
		cur = pp
	}
	return cur
}

// subtree returns root plus all its descendants (BFS).
func subtree(root int, t procTable) []int {
	out := []int{root}
	queue := []int{root}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, c := range t.children[cur] {
			out = append(out, c)
			queue = append(queue, c)
		}
	}
	return out
}

func allDead(pids []int) bool {
	for _, p := range pids {
		if isAlive(p) {
			return false
		}
	}
	return true
}

func isAlive(pid int) bool {
	ok, err := process.PidExists(int32(pid))
	return err == nil && ok
}
