# Plan 018: macOS scan robustness — socket-enumeration timeout, batched lsof, testable parser

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat d8a2b26..HEAD -- internal/scan internal/cli/doctor.go AGENTS.md internal/scan/AGENTS.md`
> Plan 005 must be DONE — this plan plugs into the `rowsFromConns`/enrich
> shape it created.
>
> **Platform caveat**: this box is Linux/WSL. Darwin code here is verified by
> cross-compilation and by unit-testing the extracted pure logic; real-Mac
> verification is tracked in `backlog/04-verify-macos-windows.md` and MUST be
> flagged in your final report as still required.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED (darwin path cannot be executed here; mitigations: pure-logic extraction + compile gates + the backlog verification item)
- **Depends on**: plans/005-scan-row-correctness.md
- **Category**: bug + perf
- **Planned at**: commit `d8a2b26`, 2026-07-09

## Why this matters

Three macOS problems, all in the scan path, all invisible from this dev box:

1. **The socket enumeration itself shells out to lsof with no timeout.**
   `gnet.Connections("inet")` on darwin resolves to gopsutil's
   `net_unix.go`, which runs `lsof -i tcp -i udp` via
   `CallLsofWithContext(context.Background(), ...)` — no deadline (verified
   in the module cache at v4.26.5). A wedged lsof hangs `whence list`, the
   TUI, and `kill` forever — precisely the failure the repo's execx invariant
   promises can't happen; it slips through because the shell-out hides inside
   a dependency.
2. **Missing lsof kills the whole scan, and the docs say otherwise.**
   An lsof-not-found error from `Connections` becomes the fatal
   `enumerate sockets:` error. `doctor` and both AGENTS files claim lsof is
   only needed for *cwd resolution* — decision-doc drift; on a Mac without
   lsof, whence is fully nonfunctional, not "rows without cwd".
3. **Per-process lsof is an N+1.** `processCwd` spawns one
   `lsof -a -p <pid> -d cwd -Fn` per process, sequentially, each costing
   100–300ms+ — 1–3s added to every list/refresh for 10 listeners. lsof
   accepts a comma-separated pid list and `-Fpn` output tags each `n` line
   with its `p` pid, so one call resolves every cwd.

## Current state

All excerpts below are from commit `d8a2b26` (post-plan 014).

- `internal/scan/scan.go:20-34` — `Processes` with no timeout and per-pid cwd:

  ```go
  func Processes() ([]model.Server, error) {
      conns, err := gnet.Connections("inet")
      if err != nil {
          return nil, fmt.Errorf("enumerate sockets: %w", err)
      }
      servers := rowsFromConns(conns, time.Now(), enrich)
      servers = collapseIPv4IPv6(servers)
      sort.Slice(servers, ...)
      return servers, nil
  }
  ```

- `internal/scan/scan.go:40-41` — `rowsFromConns` signature:

  ```go
  func rowsFromConns(conns []gnet.ConnectionStat, now time.Time,
      enrichFn func(*model.Server, int32, time.Time)) []model.Server
  ```

- `internal/scan/scan.go:109-141` — `enrich` calls `processCwd(pid)` per row
  at line 136, appending a note on error at line 139:

  ```go
  func enrich(s *model.Server, pid int32, now time.Time) {
      // ... process details ...
      if cwd, err := processCwd(pid); err == nil && cwd != "" {
          s.Cwd = cwd
      } else if err != nil {
          s.Notes = append(s.Notes, "cwd: "+err.Error())
      }
  }
  ```

- `internal/scan/cwd_darwin.go:15-37` — darwin's `processCwd` shells
  `lsof -a -p <pid> -d cwd -Fn` per pid (N calls for N processes):

  ```go
  const lsofTimeout = 2 * time.Second

  func processCwd(pid int32) (string, error) {
      out, err := execx.Output(lsofTimeout, "lsof", "-a", "-p", fmt.Sprint(pid), "-d", "cwd", "-Fn")
      if err != nil {
          return "", fmt.Errorf("lsof: %w", err)
      }
      for _, line := range strings.Split(string(out), "\n") {
          if strings.HasPrefix(line, "n") {
              return strings.TrimPrefix(line, "n"), nil
          }
      }
      return "", fmt.Errorf("cwd not found in lsof output")
  }
  ```

- `internal/scan/cwd_linux.go:13-22` — linux reads `/proc/<pid>/cwd`:

  ```go
  func processCwd(pid int32) (string, error) {
      link := fmt.Sprintf("/proc/%d/cwd", pid)
      cwd, err := os.Readlink(link)
      if err != nil {
          if os.IsPermission(err) {
              return "", fmt.Errorf("permission denied")
          }
          return "", err
      }
      return cwd, nil
  }
  ```

- `internal/scan/cwd_windows.go:14-19` — windows delegates to gopsutil.
  Structure matches linux: `processCwd(pid int32) (string, error)`.

- `internal/cli/doctor.go:53-60` — darwin lsof report (needs updating):

  ```go
  // macOS leans on lsof for cwd (and possibly socket enumeration).
  if runtime.GOOS == "darwin" {
      if path, err := exec.LookPath("lsof"); err == nil {
          row("lsof", "found at "+path)
      } else {
          row("lsof", "MISSING — cwd resolution will fail on macOS")
      }
  }
  ```

- `AGENTS.md:134` — caveat says "macOS cwd needs `lsof`" (understates —
  socket enumeration also needs it). `internal/scan/AGENTS.md:21` —
  table row says lsof is a runtime dependency for cwd (same understatement).

- Test seam pattern (from plans 001/013): package-level function vars injected
  in tests. `scan_test.go` covers `rowsFromConns` and `collapseIPv4IPv6`
  with synthetic `gnet.ConnectionStat` slices; `enrich` is tested indirectly
  through `rowsFromConns` with injected `enrichFn` fakes. There are no cwd-
  failure note tests in scan_test.go.

- gopsutil v4.26.5 exposes `gnet.ConnectionsWithContext(ctx, kind)`; on
  darwin, `CallLsofWithContext` passes ctx to `exec.CommandContext` —
  confirmed in the module cache at
  `gopsutil/v4@v4.26.5/net/net_unix.go` and
  `gopsutil/v4@v4.26.5/internal/common/common_unix.go`.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Scan tests | `go test ./internal/scan/ -v` | pass |
| Darwin compile | `GOOS=darwin GOARCH=arm64 go build ./... && GOOS=darwin GOARCH=amd64 go build ./...` | exit 0 |
| Windows compile | `GOOS=windows GOARCH=amd64 go build ./...` | exit 0 |
| Linux build/tests | `go build ./... && go test ./internal/scan/ -v` | exit 0 |
| Suite | `make lint && make test` | exit 0 |

## Scope

**In scope**:
- `internal/scan/scan.go` (scanTimeout, ConnectionsWithContext, batched-cwd hook in `Processes`)
- `internal/scan/scan_test.go` (timeout path test, cwd-note tests)
- `internal/scan/cwd_darwin.go` (batched `processCwds` → one lsof call)
- `internal/scan/cwd_linux.go` (add `processCwds` loop wrapper)
- `internal/scan/cwd_windows.go` (add `processCwds` loop wrapper)
- New `internal/scan/lsof.go` (untagged pure parser)
- New `internal/scan/lsof_test.go` (parser tests, runs on all OSes)
- `internal/cli/doctor.go:53-60` (darwin lsof wording)
- `AGENTS.md:134` (`internal/scan/AGENTS.md:21` — lsof dependency truth)

**Out of scope**:
- gopsutil version changes.
- Windows cwd mechanism changes.
- Any change to per-row error semantics beyond cwd-note preservation
  (cwd failures stay Notes, never abort the scan).

## Git workflow

- Branch: `advisor/018-macos-scan`
- Commit per step.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Bound the socket enumeration with a timeout

In `internal/scan/scan.go`, add a package-level seam var and a timeout
constant, then switch to the context-aware API. The seam follows the pattern
from `internal/kill/kill.go` (plan 001 — package-level function vars):

```go
import "context"

// scanTimeout bounds the socket enumeration. On darwin gopsutil shells out
// to lsof for this (not just for cwd), and a wedged lsof must time out
// rather than hang every command — the same rule execx enforces for our own
// shell-outs. On linux/windows the enumeration is syscalls and never
// approaches this.
const scanTimeout = 10 * time.Second

// connections is the socket-enumeration function, wrapped so tests on linux
// can verify the context plumbing without real lsof.
var connections = gnet.ConnectionsWithContext
```

In `Processes()`, replace the `gnet.Connections("inet")` call (line 21) with:

```go
ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
defer cancel()
conns, err := connections(ctx, "inet")
if err != nil {
    if ctx.Err() != nil {
        return nil, fmt.Errorf("enumerate sockets: timed out after %s: %w", scanTimeout, err)
    }
    return nil, fmt.Errorf("enumerate sockets: %w", err)
}
```

Add a test in `scan_test.go` that injects a fake `connections` and verifies
both the timeout wrapping and the normal-error pass-through. Pattern: the
table-driven tests in `kill_test.go` (plan 001). Add `"context"` to the test
file imports.

```go
func TestProcesses_TimeoutWrap(t *testing.T) {
    save := connections
    defer func() { connections = save }()
    connections = func(ctx context.Context, kind string) ([]gnet.ConnectionStat, error) {
        // Consume the context so the caller sees a deadline error.
        <-ctx.Done()
        return nil, ctx.Err()
    }
    _, err := Processes()
    if err == nil {
        t.Fatal("expected an error from a timed-out scan")
    }
    if !strings.Contains(err.Error(), "timed out after") {
        t.Errorf("error = %q, want it to contain 'timed out after'", err)
    }
}

func TestProcesses_PassesThroughNonTimeoutError(t *testing.T) {
    save := connections
    defer func() { connections = save }()
    connections = func(ctx context.Context, kind string) ([]gnet.ConnectionStat, error) {
        return nil, errors.New("lsof: command not found")
    }
    _, err := Processes()
    if err == nil {
        t.Fatal("expected an error")
    }
    if strings.Contains(err.Error(), "timed out") {
        t.Errorf("non-timeout error must not carry 'timed out': %q", err)
    }
    if !strings.Contains(err.Error(), "enumerate sockets:") {
        t.Errorf("error = %q, want 'enumerate sockets:' prefix", err)
    }
}
```

Add `"context"`, `"errors"`, and `"strings"` to the test file imports.

**Verify**: `go test ./internal/scan/ -run TestProcesses_Timeout -v` → both
pass; `go build ./...` → exit 0.

### Step 2: Extract the lsof field-output parser (pure, untagged)

New `internal/scan/lsof.go` — NO build tag, so it compiles and tests on every
OS. This is the tested core; the darwin subprocess plumbing calls it.

```go
package scan

import "strconv"

// parseLsofCwds parses `lsof -a -p <pids> -d cwd -Fpn` field output.
// lsof -F emits one field per line: 'p'-prefixed lines carry a pid,
// 'n'-prefixed lines carry the cwd path for the most recent pid.
// Returns pid → cwd for every (pid, path) pair found.
func parseLsofCwds(out []byte) map[int32]string {
    result := map[int32]string{}
    var curPid int32
    var hasPid bool
    for _, line := range strings.Split(string(out), "\n") {
        if len(line) == 0 {
            continue
        }
        switch line[0] {
        case 'p':
            if pid, err := strconv.ParseInt(line[1:], 10, 32); err == nil {
                curPid = int32(pid)
                hasPid = true
            }
        case 'n':
            if hasPid {
                result[curPid] = line[1:]
            }
        }
    }
    return result
}
```

New `internal/scan/lsof_test.go` — NO build tag:

```go
package scan

import "testing"

func TestParseLsofCwds(t *testing.T) {
    cases := []struct {
        name string
        in   string
        want map[int32]string
    }{
        {"single pid", "p123\nn/Users/x/dev/app\n",
            map[int32]string{123: "/Users/x/dev/app"}},
        {"batch", "p1\nn/a\np2\nn/b\n",
            map[int32]string{1: "a", 2: "b"}},
        {"missing cwd", "p1\np2\nn/b\n",
            map[int32]string{2: "b"}},
        {"empty input", "",
            map[int32]string{}},
        {"spaces in path", "p1\nn/Users/x/my project\n",
            map[int32]string{1: "/Users/x/my project"}},
        {"garbage lines", "garbage\np1\nn/a\nf0\n",
            map[int32]string{1: "a"}},
    }
    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            got := parseLsofCwds([]byte(tc.in))
            if len(got) != len(tc.want) {
                t.Errorf("len = %d, want %d; got=%v", len(got), len(tc.want), got)
                return
            }
            for pid, wantPath := range tc.want {
                if got[pid] != wantPath {
                    t.Errorf("pid %d: got %q, want %q", pid, got[pid], wantPath)
                }
            }
        })
    }
}
```

Add `"strconv"` and `"strings"` to `lsof.go` imports.

**Verify**: `go test ./internal/scan/ -run TestParseLsofCwds -v` → all 6
cases pass on this (Linux) box.

### Step 3: Batched cwd resolution

This is the largest step. It has three parts: (A) a `cwdResult` type so
errors survive the map lookup, (B) per-OS `processCwds` functions, and
(C) wiring the batched map into `Processes` + `enrich`.

#### Part A: `cwdResult` type

Add to `internal/scan/scan.go` (before `enrich`):

```go
// cwdResult pairs a resolved working directory with the per-pid error that
// produced it. Used to batch cwd resolution while preserving per-row notes:
// enrich writes a cwd: note when err is non-nil, and sets s.Cwd when path is
// non-empty with no error.
type cwdResult struct {
    path string
    err  error
}
```

#### Part B: Per-OS `processCwds` functions

Each `cwd_*.go` file gets a `processCwds` function that resolves all pids
at once and returns error-preserving results.

**`cwd_linux.go`** — add after `processCwd`:

```go
// processCwds resolves cwds pid-by-pid via /proc on Linux. Each per-pid
// error (permission, ENOENT) is preserved in the result so enrich can write
// the usual cwd: notes.
func processCwds(pids []int32) map[int32]cwdResult {
    out := make(map[int32]cwdResult, len(pids))
    for _, pid := range pids {
        path, err := processCwd(pid)
        out[pid] = cwdResult{path: path, err: err}
    }
    return out
}
```

**`cwd_windows.go`** — add the same function (identical implementation;
compiles per-platform). Copy the linux version; the body is the same loop
calling `processCwd`.

**`cwd_darwin.go`** — replace the entire file with a batched implementation:

```go
//go:build darwin

package scan

import (
    "fmt"
    "strings"
    "time"

    "github.com/joshmcadams/whence/internal/execx"
)

// lsofBatchTimeout bounds one batch lsof call over all process pids.
const lsofBatchTimeout = 5 * time.Second

// processCwds resolves working directories for every given pid with a
// single lsof call. lsof accepts comma-separated pids and -Fpn tags each
// n line with its p pid, so one invocation replaces N sequential calls.
//
// On error, stdout is still parsed (same partial-tolerance pattern: lsof
// exits non-zero when ANY listed pid has no cwd fd, but the rest may
// succeed). Only completely empty output is treated as a failure, recorded
// as a note on every attributed row — never a scan abort.
func processCwds(pids []int32) map[int32]cwdResult {
    if len(pids) == 0 {
        return nil
    }
    pidStrs := make([]string, len(pids))
    for i, pid := range pids {
        pidStrs[i] = fmt.Sprint(pid)
    }
    pidList := strings.Join(pidStrs, ",")

    out, err := execx.Output(lsofBatchTimeout, "lsof", "-a", "-p", pidList, "-d", "cwd", "-Fpn")

    result := make(map[int32]cwdResult, len(pids))
    parsed := parseLsofCwds(out)

    // Pids that appeared in the lsof output get their cwd.
    for pid, path := range parsed {
        result[pid] = cwdResult{path: path}
    }

    if err != nil && len(parsed) == 0 {
        // lsof failed AND produced no parseable output — the batch failed.
        // Record the error on every pid.
        batchErr := fmt.Errorf("lsof: %w", err)
        for _, pid := range pids {
            if _, seen := result[pid]; !seen {
                result[pid] = cwdResult{err: batchErr}
            }
        }
    } else {
        // Pids lsof didn't report get a specific note.
        for _, pid := range pids {
            if _, seen := result[pid]; !seen {
                result[pid] = cwdResult{err: fmt.Errorf("not reported by lsof")}
            }
        }
    }

    return result
}
```

Delete the old `lsofTimeout` constant and `processCwd` function from
`cwd_darwin.go` — they are replaced by `processCwds` + `parseLsofCwds`.

#### Part C: Wire batch resolution into `Processes` + `enrich`

In `scan.go`, change `enrich`'s signature to accept the cwd map
(add the new parameter, reorder if needed to keep `s, pid, now` first):

```go
func enrich(s *model.Server, pid int32, now time.Time, cwds map[int32]cwdResult) {
    p, err := process.NewProcess(pid)
    if err != nil {
        s.Notes = append(s.Notes, "process: "+err.Error())
        return
    }
    // ... (exe, name, cmdline, ppid, createtime — all unchanged from today) ...

    if r, ok := cwds[pid]; ok {
        if r.err != nil {
            s.Notes = append(s.Notes, "cwd: "+r.err.Error())
        } else if r.path != "" {
            s.Cwd = r.path
        }
    }
}
```

In `Processes()`, collect pids before `rowsFromConns`, call `processCwds`
once, and pass the map via a closure (preserving `rowsFromConns`'s signature):

```go
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
    sort.Slice(servers, ...)  // unchanged
    return servers, nil
}
```

Note: `rowsFromConns`'s signature is unchanged — the closure adapts the new
`enrich` signature to the old `enrichFn func(*model.Server, int32, time.Time)`.

**Tests**: Add to `scan_test.go`. First, a test that the cwd map flows into
enrich, including error-note preservation:

```go
func TestProcesses_CwdResultMapFlowsIntoEnrich(t *testing.T) {
    // Inject a fake connections so we control which pids appear.
    save := connections
    defer func() { connections = save }()
    connections = func(ctx context.Context, kind string) ([]gnet.ConnectionStat, error) {
        return []gnet.ConnectionStat{
            {Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "127.0.0.1", Port: 8080}, Pid: 42},
            {Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "127.0.0.1", Port: 9090}, Pid: 99},
            // Duplicate pid: 42 appears again on a different port. It must
            // only be resolved once by processCwds, and both rows must
            // share the result.
            {Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "127.0.0.1", Port: 8081}, Pid: 42},
        }, nil
    }
    servers, err := Processes()
    if err != nil {
        t.Fatalf("Processes: %v", err)
    }
    // The real processCwd calls on Linux will resolve /proc/self for our
    // own pid and fail for other pids. At minimum we need: all three rows
    // exist, and Notes are populated for unresolvable pids.
    if len(servers) < 2 {
        t.Fatalf("expected >= 2 servers, got %d", len(servers))
    }
    // Find the row that shares pid 42 — if it appeared twice, both rows
    // should have identical cwd (or identical cwd-failure note).
    var p42 []model.Server
    for _, s := range servers {
        if s.PID == 42 {
            p42 = append(p42, s)
        }
    }
    if len(p42) != 2 {
        t.Fatalf("expected 2 rows for pid 42, got %d", len(p42))
    }
    // Both pid-42 rows must have the same Cwd (or same cwd note if failed).
    if p42[0].Cwd != p42[1].Cwd {
        t.Errorf("pid 42 rows have different Cwd: %q vs %q — batch resolution should be once",
            p42[0].Cwd, p42[1].Cwd)
    }
}
```

Second, verify that when the fake connections returns a pid that `processCwd`
fails for (e.g. permission denied), the row still appears with a cwd note:

```go
func TestProcesses_CwdFailureBecomesNote(t *testing.T) {
    save := connections
    defer func() { connections = save }()
    connections = func(ctx context.Context, kind string) ([]gnet.ConnectionStat, error) {
        return []gnet.ConnectionStat{
            // PID 1 on Linux is init — own-user processes can't read its
            // /proc/<pid>/cwd, so processCwd will return a permission error.
            {Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "127.0.0.1", Port: 9999}, Pid: 1},
        }, nil
    }
    servers, err := Processes()
    if err != nil {
        t.Fatalf("Processes: %v", err)
    }
    // The row must exist even with a failed cwd.
    if len(servers) != 1 {
        t.Fatalf("expected 1 server, got %d", len(servers))
    }
    found := false
    for _, n := range servers[0].Notes {
        if strings.Contains(n, "cwd:") {
            found = true
            break
        }
    }
    if !found {
        t.Errorf("expected a cwd note, got notes=%v", servers[0].Notes)
    }
}
```

Add `"strings"` to test imports if not already present.

**Verify**: `go test ./internal/scan/ -v` → all tests pass (existing +
new); `GOOS=darwin GOARCH=arm64 go build ./... && GOOS=darwin GOARCH=amd64
go build ./...` → exit 0; `GOOS=windows GOARCH=amd64 go build ./...` → exit 0.

### Step 4: True up doctor and the AGENTS notes

**`internal/cli/doctor.go:53-60`** — replace the entire darwin block with:

```go
// macOS requires lsof for both socket enumeration and cwd resolution.
// gopsutil shells out to lsof inside gnet.Connections for the socket scan
// itself, not just for cwd — so without lsof, whence cannot list servers
// at all on macOS.
if runtime.GOOS == "darwin" {
    if path, err := exec.LookPath("lsof"); err == nil {
        row("lsof", "found at "+path)
    } else {
        row("lsof", "MISSING — socket enumeration and cwd resolution both require lsof on macOS")
    }
}
```

**`AGENTS.md:134`** — replace the existing caveat line:

```
- **macOS cwd needs `lsof`**; Windows cwd reads the PEB via gopsutil and is the
```

with:

```
- **macOS requires `lsof` for both socket enumeration and cwd resolution**
  (gopsutil shells out to lsof inside `gnet.Connections`); Windows cwd reads
  the PEB via gopsutil and is the
```

**`internal/scan/AGENTS.md`** — update the `cwd_darwin.go` row in the table
and add one sentence describing the batch resolver. Current line 21:

```
| `cwd_darwin.go` | `darwin` | parse `lsof -a -p <pid> -d cwd -Fn` | Makes `lsof` a **runtime dependency** on macOS (`doctor` reports it). gopsutil has no Darwin `Cwd()`. |
```

Replace with:

```
| `cwd_darwin.go` | `darwin` | batched `lsof -a -p <pids> -d cwd -Fpn` | lsof is also required for socket enumeration (gopsutil's `gnet.Connections` shells out to it). One call resolves all process cwds; errors become per-row notes via `processCwds` + `cwdResult`. |
```

**Verify**: `make lint && make test` → exit 0.

## Test plan

- `scan_test.go`: new `TestProcesses_TimeoutWrap` + `TestProcesses_PassesThroughNonTimeoutError`
  (Step 1 — verify timeout wrapping and normal-error pass-through).
- `lsof_test.go`: new `TestParseLsofCwds` (Step 2 — 6 cases, cross-platform).
- `scan_test.go`: new `TestProcesses_CwdResultMapFlowsIntoEnrich` +
  `TestProcesses_CwdFailureBecomesNote` (Step 3 — verify batch map flows into
  rows and cwd failures stay Notes).
- All pre-existing tests must pass unchanged (the `steps` closure in `rowsFromConns`
  tests already inject their own `enrichFn` and won't call `processCwd`).
- Pattern: existing table-driven tests in `scan_test.go`.
- Darwin subprocess plumbing is compile-gated only — flag this in the report.

## Done criteria

- [ ] `grep -n "gnet.Connections(" internal/scan/scan.go` → no matches
      (the context variant `connections(ctx, "inet")` is used instead)
- [ ] `internal/scan/lsof.go` has no build tag; `TestParseLsofCwds` passes on Linux
- [ ] `TestProcesses_TimeoutWrap` passes (fake context timeout → error says "timed out after")
- [ ] `TestProcesses_CwdFailureBecomesNote` passes (pid 1 row has a cwd note)
- [ ] Darwin `processCwds` makes exactly one lsof invocation — confirmed in code review
      (the batch call is a single `execx.Output`; note this in the report)
- [ ] doctor + both AGENTS files state that lsof is required for the scan itself
- [ ] `GOOS=darwin GOARCH=arm64 go build ./... && GOOS=darwin GOARCH=amd64 go build ./...`
      → exit 0; `GOOS=windows GOARCH=amd64 go build ./...` → exit 0
- [ ] `make lint && make test` exit 0 (all packages, uncached)
- [ ] Final report flags real-Mac verification as outstanding (backlog 04)
- [ ] `plans/README.md` status row updated

## STOP conditions

- Plan 005 not DONE. Verify: `grep -q "rowsFromConns" internal/scan/scan.go` → exit 0
  (the function exists; plan 005 created it).
- `gnet.ConnectionsWithContext` does not exist or is not the function signature
  assumed here. Verify: `go doc github.com/shirou/gopsutil/v4/net ConnectionsWithContext`
  → the first parameter is `ctx context.Context`, the second is `kind string`.
- Pre-existing `rowsFromConns` tests fail after the closure change — they
  inject their own `enrichFn` fakes and must not call `processCwd` at all;
  if one does, the `processCwds` call in `Processes` may fail on the fake
  pids. Report the failing test name and the pid it uses.
- The `run` keyword test (`TestProcesses_CwdFailureBecomesNote`) produces a
  real process error instead of a cwd note — pid 1 may be readable on some
  kernels; adjust the test to use a different known-inaccessible pid (e.g.
  pid 2 on linux if needed).
- Anything requires actually running lsof on this box — the parser tests
  and cross-compilation are the executable surface.

## Maintenance notes

- Real-Mac validation checklist for backlog 04: `whence list` shows cwd-based
  attribution; `whence doctor` reports lsof; kill a Vite dev server; a
  deliberately slow lsof times the scan out at ~10s with the "timed out after"
  message instead of hanging.
- `scanTimeout` applies to all platforms (shared `scan.go`) even though only
  darwin's implementation can actually hit it — linux/windows syscalls never
  approach it.
- `lsofBatchTimeout` (5s) is darwin-only. If Macs with many listeners report
  timeouts, increase it; the per-pid 2s timeout was a floor for N+1 calls and
  a single batch can afford more time.
- If gopsutil ever implements native darwin cwd (`proc_pidinfo`), the lsof
  subprocess call inside `processCwds` can be replaced with that call while
  keeping the batch-resolver structure (`processCwds` + `cwdResult` + the
  closure in `Processes`). The `parseLsofCwds` function and the `lsof.go`
  file would be deleted in that case, but the rest of the architecture
  survives.
