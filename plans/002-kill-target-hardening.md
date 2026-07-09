# Plan 002: Harden kill targeting — identity re-check, cycle guards, zombie-aware liveness

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/kill internal/cli/kill.go internal/tui/tui.go internal/model/model.go`
> Plan 001 is EXPECTED to have landed (seams + tests). If plan 001 is not
> DONE in `plans/README.md`, STOP — this plan depends on its tests.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED (changes the kill path; mitigated by plan 001's tests)
- **Depends on**: plans/001-kill-path-characterization-tests.md
- **Category**: bug + security
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Three defects let `whence kill` hit the wrong target or misreport:

1. **PID-reuse race**: the PID captured at scan time is signaled after an
   *unbounded* wait at the confirmation prompt, with no check that the PID
   still belongs to the same process. The scanner already collects
   `Server.StartTime`, but the kill never compares it. If the target exits
   while the user reads the prompt and the OS recycles the PID, whence kills
   an unrelated same-user process — and `climb` then expands the blast radius
   from the wrong root.
2. **No cycle guards**: `climb` and `subtree` trust raw ppid data. A cycle
   (possible from mid-snapshot PID reuse; ppids also go stale on Windows,
   where a dead parent's pid can be recycled) hangs the kill forever or
   attaches an unrelated process to the kill tree.
3. **Zombies count as alive**: `isAlive` uses gopsutil `PidExists`, which on
   Linux stats `/proc/<pid>` — a zombie's entry exists. A killed process whose
   parent doesn't reap it makes the wait loop burn the full grace period and
   then report `processes survived SIGKILL` for a kill that succeeded.

## Current state

- `internal/kill/kill.go` — after plan 001 it has seams
  (`takeSnapshot`, `terminatePID`, `forceKillPID`, `pidAlive`). The logic is
  otherwise as written at `caec51a`:

  ```go
  // kill.go:255-268 — no cycle guard, no iteration cap
  func climb(pid int, t procTable) int {
      cur := pid
      for {
          pp, ok := t.ppid[cur]
          if !ok || pp <= 1 { break }
          if !launchers[t.name[pp]] { break }
          cur = pp
      }
      return cur
  }

  // kill.go:271-283 — BFS with no visited set
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

  // kill.go:294-297
  func isAlive(pid int) bool {
      ok, err := process.PidExists(int32(pid))
      return err == nil && ok
  }
  ```

- `kill.Server` signature (kill.go:144): `func Server(s model.Server, o Opts) Result`.
  `model.Server` already carries `StartTime time.Time` (set by
  `internal/scan/scan.go:125-127` from gopsutil `CreateTime`, and by
  `internal/docker/docker.go` for containers). `killProcess(pid int, o Opts)`
  receives only the pid today.

- `internal/kill/AGENTS.md` — invariants: `climb` must stop at any
  non-launcher parent and at pid ≤ 1; preview and kill share `planTree`.
  Nothing in this plan may weaken those.

- Callers of `kill.Server`: `internal/cli/kill.go:86` and
  `internal/tui/tui.go:128-133` (`killCmd`). Both pass the full
  `model.Server`, so the identity data is already available — no caller
  signature changes needed.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Build | `go build ./...` | exit 0 |
| Kill pkg tests | `go test ./internal/kill/ -v` | all pass |
| Full suite | `make test` | ok |
| Lint | `make lint` | exit 0 |
| Cross-compile check | `GOOS=windows GOARCH=amd64 go build ./...` | exit 0 |

## Scope

**In scope**:
- `internal/kill/kill.go`
- `internal/kill/kill_test.go`
- `internal/kill/AGENTS.md` (one paragraph documenting the identity check)

**Out of scope**:
- `internal/kill/signal_unix.go` / `signal_windows.go` — signal mechanics unchanged.
- The `launchers` map — curated list, do not add or remove entries.
- `internal/cli/kill.go`, `internal/tui/tui.go` — no caller changes; the new
  refusal error surfaces through the existing `Result.Err` display paths.
- Any change to `Preview`'s freshness semantics (it deliberately re-snapshots).

## Git workflow

- Branch: `advisor/002-kill-hardening`
- One commit per step; short imperative messages.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Cycle guards in `climb` and `subtree`

- `subtree`: add a `seen map[int]bool`; skip children already seen; the root
  is seen at start. Output order (BFS, root first) must not change for
  acyclic input — plan 001/existing tests must pass untouched.
- `climb`: add a `seen map[int]bool` (or a hop cap of, say, 64); stop climbing
  when the next parent was already visited.

Add tests: a synthetic `procTable` where `ppid` forms a cycle between two
launcher-named pids (e.g. 100→200, 200→100, both named `npm`) must (a) return
from `climb` and (b) return from `subtree` with each pid at most once. Use a
test timeout guard (the test itself hanging = failure).

**Verify**: `go test ./internal/kill/ -run 'TestClimb|TestSubtree|TestPlanTree' -v`
→ all pass, including pre-existing cases unchanged.

### Step 2: Zombie-aware liveness

Change `isAlive` to treat zombies as dead:

```go
func isAlive(pid int) bool {
    ok, err := process.PidExists(int32(pid))
    if err != nil || !ok {
        return false
    }
    p, err := process.NewProcess(int32(pid))
    if err != nil {
        return false
    }
    if statuses, err := p.Status(); err == nil {
        for _, st := range statuses {
            if st == process.Zombie {
                return false
            }
        }
    }
    return true
}
```

Notes: gopsutil v4 `Status()` returns `[]string`; the zombie constant is
`process.Zombie` (`"zombie"`). On Windows `Status()` returns an
ErrNotImplemented-style error — the `err == nil` guard above means Windows
behavior is unchanged (PidExists only). Do not build-tag this; the guard is
the cross-platform story.

Because `isAlive` is behind the plan-001 seam (`pidAlive`), existing fakes
keep working. Add one test: a fake is NOT possible here (real gopsutil call),
so test indirectly — spawn a real child that becomes a zombie:
`cmd := exec.Command("sleep", "0.01"); cmd.Start(); time.Sleep(100ms)` —
without calling `cmd.Wait()`, the child is a zombie; assert
`isAlive(cmd.Process.Pid) == false`; then `cmd.Wait()` to clean up. Guard the
test with `//go:build` nothing — it runs on Linux CI; use
`runtime.GOOS == "windows"`-skip via `t.Skipf` if needed (tests are the one
place a GOOS check is acceptable).

**Verify**: `go test ./internal/kill/ -run TestIsAliveZombie -v` → pass.

### Step 3: Identity re-check before signaling

Thread the scanned identity into the kill:

- Change `killProcess(pid int, o Opts)` to
  `killProcess(s model.Server, o Opts)` (it is unexported; only
  `Server` calls it). Use `s.PID` where `pid` was used.
- Before the terminate loop, when `s.StartTime` is non-zero, verify identity:

  ```go
  // verifyIdentity reports whether pid still refers to the process scanned
  // earlier. Best-effort: an unreadable create time (permissions, races)
  // fails OPEN only when the scan had no start time to compare against.
  var processCreateTime = func(pid int) (time.Time, error) {
      p, err := process.NewProcess(int32(pid))
      if err != nil { return time.Time{}, err }
      ms, err := p.CreateTimeWithContext(context.Background())
      if err != nil { return time.Time{}, err }
      return time.UnixMilli(ms), nil
  }

  func verifyIdentity(pid int, scanned time.Time) error {
      if scanned.IsZero() { return nil } // nothing to compare
      now, err := processCreateTime(pid)
      if err != nil {
          return fmt.Errorf("target changed since scan (pid %d no longer readable): %w — rescan and retry", pid, err)
      }
      const epsilon = 2 * time.Second // clock-source granularity differs per OS
      if d := now.Sub(scanned); d > epsilon || d < -epsilon {
          return fmt.Errorf("target changed since scan (pid %d was reused by another process) — rescan and retry", pid)
      }
      return nil
  }
  ```

  If `verifyIdentity(s.PID, s.StartTime)` returns an error, `killProcess`
  returns it immediately — nothing is signaled.
- The check protects the *root* target. Tree members from the fresh
  `takeSnapshot()` are already current-as-of-now; do not add per-member
  create-time checks (that would double the snapshot cost for marginal gain —
  the dangerous stale datum is the scanned PID).
- `processCreateTime` is a seam var (same pattern as plan 001) so tests can
  fake it.

Tests (extend plan 001's fixtures):
1. Matching create time (fake returns `s.StartTime`) → kill proceeds exactly
   as before (reuse a plan-001 graceful-success scenario).
2. Mismatched create time (fake returns `s.StartTime + time.Minute`) →
   `Result.Err` contains `target changed since scan`, `terminatePID` never
   called, `Killed == false`.
3. Zero `StartTime` on the server → no check, kill proceeds (covers
   docker-proxy-attributed odd rows and any scan path that failed to read
   CreateTime).
4. Create-time read error with non-zero scanned time → refusal error,
   nothing signaled.

**Verify**: `go test ./internal/kill/ -v` → all pass;
`GOOS=windows GOARCH=amd64 go build ./...` and
`GOOS=darwin GOARCH=arm64 go build ./...` → exit 0.

### Step 4: Document the new behavior

In `internal/kill/AGENTS.md`, add a short paragraph under the invariants:
kills verify the scanned PID's create time (±2s) before signaling and refuse
with "target changed since scan" on mismatch; zombies count as dead for the
wait loop; `climb`/`subtree` are cycle-safe. Keep it to ~5 lines, matching the
file's terse style.

**Verify**: `make lint && make test` → exit 0.

## Test plan

Steps 1–3 each carry their tests (that's the core deliverable): cycle-table
cases for `climb`/`subtree`, one real-zombie liveness test, and four
identity-check scenarios through `kill.Server` using the plan-001 seams.
Model all fakes after the existing synthetic-`procTable` style in
`internal/kill/kill_test.go`.

## Done criteria

- [ ] `make lint` and `make test` exit 0
- [ ] `go test ./internal/kill/ -run TestClimbCycle -v` passes (and completes in < 5s — no hang)
- [ ] Identity-mismatch test proves `terminatePID` is never called on refusal
- [ ] Windows + darwin cross-compiles succeed
- [ ] Plan 001's tests pass with changes ONLY where this plan specifies new behavior
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back if:

- Plan 001 is not DONE (this plan's safety depends on its tests).
- gopsutil `process.Status()` or `process.Zombie` doesn't exist in the pinned
  version (`v4.26.5`) with the shapes described — check
  `~/go/pkg/mod/github.com/shirou/gopsutil/v4@v4.26.5/process/process.go`
  before improvising an alternative.
- The identity check as specified would refuse kills in the normal happy path
  on this machine (test with a real `whence kill` against a disposable
  `python3 -m http.server 8931` — it must kill cleanly).
- Any change would touch the `launchers` map or let `climb` pass a
  non-launcher parent — invariant violation.

## Maintenance notes

- The ±2s epsilon exists because CreateTime granularity differs per OS
  (Linux jiffies vs Windows FILETIME). If false "target changed" refusals are
  reported, widen the epsilon rather than removing the check.
- If a future plan adds per-tree-member identity checks, benchmark the
  snapshot cost first — the TUI calls previews synchronously (see plan 009/015).
- Reviewer focus: confirm nothing is signaled on any refusal path, and that
  the refusal error reads clearly in both the CLI (`✗ :3000 app [pid 123] — …`)
  and the TUI status line.
