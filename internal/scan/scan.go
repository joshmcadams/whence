// Package scan enumerates listening TCP ports and the processes behind them.
package scan

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"

	"github.com/joshmcadams/whence/internal/model"
)

// scanTimeout bounds the socket enumeration. On darwin gopsutil shells out
// to lsof for this (not just for cwd), and a wedged lsof must time out
// rather than hang every command — the same rule execx enforces for our own
// shell-outs. On linux/windows the enumeration is syscalls and never
// approaches this.
const scanTimeout = 10 * time.Second

// connections is the socket-enumeration function, wrapped so tests on linux
// can verify the context plumbing without real lsof.
var connections = gnet.ConnectionsWithContext

// Processes scans all listening TCP sockets and returns one Server per
// (port, proto, address, pid). Errors fetching details for an individual
// process are recorded in Server.Notes rather than aborting the whole scan.
func Processes() ([]model.Server, error) {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()
	conns, err := connections(ctx, "inet")
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("enumerate sockets: timed out after %s: %w", scanTimeout, err)
		}
		return nil, fmt.Errorf("enumerate sockets: %w", err)
	}

	// Collect unique attributed pids for batched cwd resolution.
	pidSet := map[int32]bool{}
	for _, c := range conns {
		if strings.ToUpper(c.Status) == "LISTEN" && c.Pid > 0 {
			pidSet[c.Pid] = true
		}
	}
	pids := make([]int32, 0, len(pidSet))
	for pid := range pidSet {
		pids = append(pids, pid)
	}
	cwds := processCwds(pids)

	now := time.Now()
	servers := rowsFromConns(conns, now, func(s *model.Server, pid int32, t time.Time) {
		enrich(s, pid, t, cwds)
	})
	servers = collapseIPv4IPv6(servers)
	sort.Slice(servers, func(i, j int) bool {
		if servers[i].Port != servers[j].Port {
			return servers[i].Port < servers[j].Port
		}
		return servers[i].Proto < servers[j].Proto
	})
	return servers, nil
}

// rowsFromConns converts raw connection stats into unenriched server rows:
// LISTEN filtering, per-(port,proto,address,pid) dedup, and the no-PID note.
// enrichFn is called for rows with a PID; injected for testability.
func rowsFromConns(conns []gnet.ConnectionStat, now time.Time,
	enrichFn func(*model.Server, int32, time.Time)) []model.Server {
	seen := map[string]bool{}
	var servers []model.Server

	for _, c := range conns {
		if strings.ToUpper(c.Status) != "LISTEN" {
			continue
		}
		proto := protoOf(c.Family)
		key := fmt.Sprintf("%d/%s/%s/%d", c.Laddr.Port, proto, c.Laddr.IP, c.Pid)
		if seen[key] {
			continue
		}
		seen[key] = true

		s := model.Server{
			Port:    int(c.Laddr.Port),
			Proto:   proto,
			Address: c.Laddr.IP,
			Source:  model.SourceProcess,
			PID:     int(c.Pid),
		}
		if c.Pid > 0 {
			enrichFn(&s, c.Pid, now)
		} else {
			s.Notes = append(s.Notes, "no pid (owned by another user; rerun with elevated privileges)")
		}
		servers = append(servers, s)
	}
	return servers
}

// collapseIPv4IPv6 merges (port, pid) pairs where the tcp and tcp6 entries
// have the same exposure class (both all-interfaces or both loopback). This is
// the common dual-stack case — Vite, most Node servers — where two identical
// rows appear for one server. The surviving entry is normalized to the IPv4
// representation (proto=tcp, address=0.0.0.0 or 127.0.0.1).
// Entries with PID ≤ 0 (unattributed) and genuinely distinct IP bindings are
// left untouched.
func collapseIPv4IPv6(servers []model.Server) []model.Server {
	type key struct{ port, pid int }
	idx := map[key]int{} // key → index in out
	out := make([]model.Server, 0, len(servers))
	for _, s := range servers {
		if s.PID > 0 {
			k := key{s.Port, s.PID}
			if i, exists := idx[k]; exists {
				existing := &out[i]
				if s.Exposure() == existing.Exposure() {
					switch s.Exposure() {
					case "all":
						existing.Proto = "tcp"
						existing.Address = "0.0.0.0"
					case "local":
						existing.Proto = "tcp"
						existing.Address = "127.0.0.1"
					}
					continue
				}
			} else {
				idx[k] = len(out)
			}
		}
		out = append(out, s)
	}
	return out
}

// cwdResult pairs a resolved working directory with the per-pid error that
// produced it. Used to batch cwd resolution while preserving per-row notes:
// enrich writes a cwd: note when err is non-nil, and sets s.Cwd when path is
// non-empty with no error.
type cwdResult struct {
	path string
	err  error
}

// enrich fills process-level detail, accumulating non-fatal notes.
func enrich(s *model.Server, pid int32, now time.Time, cwds map[int32]cwdResult) {
	p, err := process.NewProcess(pid)
	if err != nil {
		s.Notes = append(s.Notes, "process: "+err.Error())
		return
	}
	if exe, err := p.Exe(); err == nil {
		s.Exe = exe
	}
	// Prefer the executable's basename: gopsutil's Name() reads /proc/pid/comm,
	// which can be a thread name (e.g. "MainThread" for a node/python server).
	if s.Exe != "" {
		s.Name = filepath.Base(s.Exe)
	} else if name, err := p.Name(); err == nil {
		s.Name = name
	}
	if cmd, err := p.Cmdline(); err == nil {
		s.Cmdline = cmd
	}
	if ppid, err := p.Ppid(); err == nil {
		s.PPID = int(ppid)
	}
	if ms, err := p.CreateTime(); err == nil && ms > 0 {
		s.StartTime = time.UnixMilli(ms)
		s.Uptime = now.Sub(s.StartTime)
	}
	if r, ok := cwds[pid]; ok {
		if r.err != nil {
			s.Notes = append(s.Notes, "cwd: "+r.err.Error())
		} else if r.path != "" {
			s.Cwd = r.path
		}
	}
}

func protoOf(family uint32) string {
	switch family {
	case 2: // AF_INET
		return "tcp"
	case 10, 23, 30: // AF_INET6 across linux/windows/darwin
		return "tcp6"
	default:
		return "tcp"
	}
}
