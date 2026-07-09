# Plan 001: Characterization tests for the kill execution path

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/kill internal/cli/kill.go internal/cli/kill_test.go internal/tui/tui.go internal/tui/tui_test.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED (introduces seams into the kill path itself; behavior must not change)
- **Depends on**: none
- **Category**: tests
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

`whence` exists to kill the right processes safely, yet the code that actually
sends signals — `killProcess`'s SIGTERM → wait → SIGKILL escalation,
`dockerStop`, the CLI confirmation gate, and the TUI's "y" commit path — has
zero test coverage (verified with `go test -coverpkg=./... -coverprofile`).
`internal/kill/kill.go` is also the highest-churn file in the repo. Plan 002
changes this exact code; these characterization tests must pin today's behavior
first so that 002's diff is reviewable as "intended change only".

## Current state

- `internal/kill/kill.go` — kill engine. The *targeting* logic (`climb`,
  `subtree`, `planTree`) is well tested against synthetic process tables in
  `internal/kill/kill_test.go`. The *execution* logic is not:

  ```go
  // kill.go:155-195 (abridged)
  func killProcess(pid int, o Opts) (string, error) {
      tbl := snapshot()
      method := "tree"
      if o.Single { method = "single" }
      tree := planTree(pid, o.Single, tbl)
      for _, p := range tree {
          _ = terminate(p) // best effort; some may already be gone
      }
      timeout := o.Timeout
      if timeout <= 0 { timeout = 5 * time.Second }
      deadline := time.Now().Add(timeout)
      for time.Now().Before(deadline) {
          if allDead(tree) { return method, nil }
          time.Sleep(100 * time.Millisecond)
      }
      var lastErr error
      for _, p := range tree {
          if isAlive(p) {
              if err := forceKill(p); err != nil { lastErr = err }
          }
      }
      if !allDead(tree) {
          if lastErr == nil { lastErr = errors.New("processes survived SIGKILL") }
          return method, lastErr
      }
      return method, nil
  }
  ```

  ```go
  // kill.go:197-212
  func dockerStop(s model.Server, o Opts) Result {
      if s.Name == "" {
          return Result{Server: s, Err: errors.New("no container name")}
      }
      secs := int(o.Timeout.Seconds())
      if secs <= 0 { secs = 5 }
      timeout := time.Duration(secs)*time.Second + 10*time.Second
      if out, err := execx.CombinedOutput(timeout, "docker", "stop", "-t", strconv.Itoa(secs), s.Name); err != nil {
          return Result{Server: s, Method: "docker stop", Err: fmt.Errorf("%v: %s", err, out)}
      }
      return Result{Server: s, Killed: true, Method: "docker stop"}
  }
  ```

  `terminate`/`forceKill` are build-tagged (`signal_unix.go` sends
  `syscall.SIGTERM`/`SIGKILL`; `signal_windows.go` uses `taskkill`). `isAlive`
  (kill.go:294-297) wraps `process.PidExists`. `snapshot()` (kill.go:222-241)
  reads the real host process table via gopsutil.

- `internal/cli/kill.go` — `runKill` (lines 46-98) hardwires `config.Load()`,
  `collect(cfg)`, `kill.Server`, and `os.Stdin`/stdout/stderr. `confirm()`
  (lines 210-223) reads a line from `os.Stdin`; EOF or anything but
  `y`/`yes` (case-insensitive, trimmed) returns false. `confirmKill`
  (lines 168-188) prints either `About to kill %d target(s) matching %q`
  (exact) or `No exact match for %q; %d server(s) contain it` (fuzzy),
  appends ` — %d process(es) total` when the batch preview found any tree
  members, then prompts `Proceed? [y/N] `. The `--single`-with-multiple-units
  warning (lines 64-68) goes to `os.Stderr`. `matchTargets`/`dedupeUnits`
  are already tested in `internal/cli/kill_test.go` — leave those tests alone.

- `internal/tui/tui.go` — in `modeConfirm`, key `y`/`yes` (lines 226-230) sets
  `m.mode = modeList`, `m.status = "killing " + describe(m.selected) + "…"`,
  and returns `killCmd(m.selected, opts)`. `killedMsg` handling (lines
  174-180) renders `✗ ...` (error) or `✓ killed ...` into `m.status` and
  returns `loadCmd(m.cfg)`. `internal/tui/tui_test.go` tests the *cancel*
  path only (`TestConfirmDeclineDoesNotKill`-style around lines 76-85) and
  pumps `Update()` directly, discarding returned commands via a `step()`
  helper — match that style.

- Conventions: white-box tests, same package, table-driven where natural.
  The synthetic `procTable` pattern to copy is in `internal/kill/kill_test.go`
  (builds `procTable{ppid: ..., name: ..., children: ...}` by hand). Short
  real-time timeouts in tests follow `internal/execx/execx_test.go` (50ms
  deadlines with generous tolerance).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Build | `go build ./...` | exit 0 |
| Kill pkg tests | `go test ./internal/kill/` | ok |
| CLI pkg tests | `go test ./internal/cli/` | ok |
| TUI pkg tests | `go test ./internal/tui/` | ok |
| Full suite | `make test` | all packages ok |
| Lint | `make lint` | exit 0 |

## Scope

**In scope** (the only files you should modify):
- `internal/kill/kill.go` — seams only (package-level function vars); no behavior change
- `internal/kill/kill_test.go` — new tests
- `internal/cli/kill.go` — extract `runKill` body into an injectable form; no behavior change
- `internal/cli/kill_test.go` — new tests
- `internal/tui/tui_test.go` — new tests (no change to `tui.go`)

**Out of scope** (do NOT touch, even though they look related):
- `internal/kill/signal_unix.go`, `internal/kill/signal_windows.go` — the real
  signal senders stay exactly as they are.
- Any behavior change to climb/subtree/planTree or the launchers list — that is
  plan 002's territory, and `internal/kill/AGENTS.md` invariants apply.
- `internal/cli/collect.go` — plan 013 turns it into a seam; leave it alone here.
- The wording of any user-visible message — tests pin the wording as-is.

## Git workflow

- Branch: `advisor/001-kill-path-tests`
- Commit style: short imperative summary, matching recent history (e.g.
  `imp-05: test the untested core packages`). One commit per step is fine.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add seams to `internal/kill` without changing behavior

In `kill.go`, introduce package-level function variables and route the
existing call sites through them:

```go
// Test seams: production code always uses these vars, tests swap them.
var (
    takeSnapshot = snapshot
    terminatePID = terminate
    forceKillPID = forceKill
    pidAlive     = isAlive
    dockerCombinedOutput = execx.CombinedOutput
)
```

Update call sites only: `killProcess` uses `takeSnapshot()`, `terminatePID(p)`,
`forceKillPID(p)`; `allDead` uses `pidAlive(p)`; `Preview`/`PreviewBatch` use
`takeSnapshot()`; `dockerStop` uses `dockerCombinedOutput(...)`. Do not rename
or change the underlying functions.

**Verify**: `go build ./... && go test ./internal/kill/` → exit 0, existing
tests pass unchanged.

### Step 2: Characterization tests for `killProcess` via `kill.Server`

In `kill_test.go`, add tests that swap the seams (restore with `t.Cleanup`)
and drive `Server(model.Server{Source: model.SourceProcess, PID: ...}, Opts{...})`
with a small timeout (e.g. `300 * time.Millisecond`):

1. **Graceful success**: fake `terminatePID` records signaled pids; fake
   `pidAlive` returns false after terminate was called → `Result.Killed == true`,
   `Method == "tree"`, every pid in the synthetic tree (root + descendants from
   a fake `takeSnapshot`) was terminated exactly once, and `forceKillPID` was
   never called.
2. **Escalation**: `pidAlive` stays true until `forceKillPID` is called, then
   false → `Killed == true`, force-kill was called for every surviving pid,
   and the call happened only after the deadline (assert elapsed ≥ timeout).
3. **Survivor**: `pidAlive` always true, `forceKillPID` returns nil →
   `Result.Err` is non-nil with message `processes survived SIGKILL`,
   `Killed == false`.
4. **Single mode**: `Opts{Single: true}` → `Method == "single"` and only the
   listening pid is signaled even when the synthetic table has a launcher
   parent and children.
5. **Default timeout**: `Opts{Timeout: 0}` — do NOT run the full 5s; instead
   assert via the graceful-success fake (dead immediately) that the call
   returns quickly and succeeds, confirming zero-timeout doesn't mean
   zero-wait-crash.
6. **No PID**: `Server(model.Server{PID: 0}, ...)` → error containing
   `no accessible pid`.

**Verify**: `go test ./internal/kill/ -run 'TestServer|TestKillProcess' -v` →
all new tests pass; `go test ./internal/kill/` still green.

### Step 3: Characterization tests for `dockerStop`

Swap `dockerCombinedOutput` with a fake capturing `(timeout, name, args)`:

1. Success path: `Server{Source: SourceDocker, Name: "web-1"}`,
   `Opts{Timeout: 7s}` → args exactly `["stop", "-t", "7", "web-1"]`, binary
   `docker`, call timeout `17s` (7s + 10s), `Killed == true`,
   `Method == "docker stop"`.
2. Zero timeout clamps to 5: `Opts{}` → `-t 5`, call timeout `15s`.
3. Failure: fake returns `([]byte("boom"), errors.New("exit 1"))` →
   `Result.Err` non-nil, message contains both `exit 1` and `boom`.
4. Empty name → error `no container name`, fake never called.

**Verify**: `go test ./internal/kill/ -run TestDockerStop -v` → pass.

### Step 4: Make `runKill` testable and pin the confirmation gate

In `internal/cli/kill.go`, extract the body of `runKill` into:

```go
type killDeps struct {
    cfg     config.Config
    servers []model.Server
    kill    func(model.Server, kill.Opts) kill.Result
    in      io.Reader
    out     io.Writer
    errOut  io.Writer
}

func runKillWith(target string, o *killOpts, d killDeps) error
```

`runKill` becomes a thin wrapper that loads config, collects, and calls
`runKillWith` with `kill.Server`, `os.Stdin`, `os.Stdout`, `os.Stderr`.
`confirm`, `confirmKill`, and `printPlan` must write to / read from the
injected streams (thread `io.Writer`/`io.Reader` parameters or methods —
smallest diff wins). Every user-visible string stays byte-identical.

Note: `confirmKill` calls `kill.PreviewBatch`, which hits the real process
table. For CLI tests that only assert prompt wording and gating, that is
acceptable (native fake servers with PID ≤ 0 short-circuit to `NoPID` plans;
docker-source fakes short-circuit to `Docker` plans — use those in test
fixtures so no real snapshot content leaks into assertions).

Add tests in `internal/cli/kill_test.go` using docker-source fixtures
(`model.Server{Source: model.SourceDocker, Name: "web-1", Port: 3000}`):

1. `--force` skips confirmation: fake kill succeeds → output contains
   `✓ killed`, no `Proceed?` prompt in output, kill func called once.
2. Non-"y" answer aborts: `in` = `"n\n"` → output contains `Proceed? [y/N]`
   and `Aborted.`, kill func never called, returned error is nil.
3. EOF aborts: `in` = empty reader → `Aborted.`, kill never called.
4. `"y"` proceeds: kill called for each deduped unit.
5. Exact wording: exact-match target → output contains
   `About to kill 1 target(s) matching`; fuzzy target (substring only) →
   `No exact match for`.
6. Failure aggregation: two units, fake kill fails for one → output has one
   `✗` and one `✓`, returned error message is `1 of 2 kill(s) failed`.
7. `--single` multi-unit warning: two units + `single: true`, non-numeric
   target → `errOut` contains `note: --single with 2 matched targets`.

**Verify**: `go test ./internal/cli/` → all pass, including the pre-existing
`matchTargets`/`dedupeUnits` tests, untouched.

### Step 5: TUI commit-path tests

In `internal/tui/tui_test.go` (match the existing `step()`/`Update()` pumping
style; no changes to `tui.go`):

1. **Confirm-yes dispatches**: build a model in `modeConfirm` with a selected
   docker-source server (avoids a real process snapshot), send key `y` →
   model returns to `modeList`, `m.status` contains `killing`, and the
   returned `tea.Cmd` is non-nil. Do NOT execute the returned command.
2. **killedMsg success**: pump `killedMsg{res: kill.Result{Server: s}}` →
   status contains `✓ killed`, returned cmd non-nil (the reload).
3. **killedMsg error**: pump `killedMsg{res: kill.Result{Server: s, Err: errors.New("nope")}}`
   → status contains `✗` and `nope`.

**Verify**: `go test ./internal/tui/` → pass.

### Step 6: Full gate

**Verify**: `make lint && make test` → both exit 0.

## Test plan

Covered by steps 2–5 above (that is the deliverable). Structural patterns:
synthetic-table style from `internal/kill/kill_test.go`, Update-pumping style
from `internal/tui/tui_test.go`, short-deadline style from
`internal/execx/execx_test.go`.

## Done criteria

- [ ] `make lint` exits 0
- [ ] `make test` exits 0
- [ ] `go test ./internal/kill/ -cover` reports ≥ 70% (was 21.4%)
- [ ] `go test ./internal/cli/ -cover` reports ≥ 45% (was 16.4%)
- [ ] No user-visible string changed: `git diff caec51a..HEAD -- internal/kill internal/cli | grep '^-.*"' ` shows no deleted message literals that aren't re-added verbatim
- [ ] No files outside the in-scope list are modified (`git status`)
- [ ] `plans/README.md` status row updated

## STOP conditions

Stop and report back (do not improvise) if:

- The code at the locations in "Current state" doesn't match the excerpts.
- Making `dockerStop` or `killProcess` testable seems to require changing their
  observable behavior (signal order, timeout arithmetic, message text) — this
  plan is characterization only.
- The TUI "y" test cannot avoid touching the real process table with a
  docker-source fixture.
- A step's verification fails twice after a reasonable fix attempt.

## Maintenance notes

- Plan 002 modifies `killProcess` and the tree helpers; these tests are its
  safety net. Reviewers of 002 should see test *updates* only where 002's spec
  explicitly changes behavior (identity check, zombie handling).
- The seam vars (`takeSnapshot`, `terminatePID`, …) are for white-box tests
  only; production code must never reassign them.
- Deferred: a real-process integration test (spawn `sleep`, kill it) was
  considered and left out to keep the suite hermetic; add one later if flake
  budget allows.
