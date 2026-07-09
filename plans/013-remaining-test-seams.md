# Plan 013: Remaining test seams — TUI preview, classify orchestration, CLI layer

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/tui internal/kill/kill.go internal/classify internal/cli/collect.go internal/cli/list.go internal/cli/config.go`
> Plan 001 must be DONE (this plan extends its seam pattern). Plans 003/009
> may have edited `tui.go` — reconcile excerpts before proceeding.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW-MED (one small public API addition to `internal/kill`; the rest is seams + tests)
- **Depends on**: plans/001-kill-path-characterization-tests.md
- **Category**: tests (+ one perf fix that falls out naturally)
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Four coverage gaps remain after plan 001, each hiding regressable behavior:

1. **The TUI blast-radius preview cannot be tested against synthetic trees.**
   Pressing `x` calls `kill.Preview`, which snapshots the REAL host process
   table — the existing `TestConfirmPreviewsBlastRadius` passes only because
   fake PID 100 happens to be a kernel thread on test hosts. The safety
   property ("the confirmation can't understate the blast radius") is barely
   asserted. Bonus defect in the same code: the `s` scope toggle re-snapshots
   the entire process table on every press, and `x` runs the snapshot
   synchronously on the event loop — one public helper fixes both.
2. **`classify.Process` (the function `inventory.Collect` actually calls) is
   untested** — its docker-skip and empty-cwd guards could silently regress
   while the well-tested scoring pieces keep passing.
3. **The CLI list/config layer is at 0%**: the hidden-count arithmetic, the
   `--no-ignore` copy semantics, and the `config --init` refuse-to-overwrite
   guard are real logic with no assertions.
4. `inventory.Sort`'s `uptime`/`name` comparators never execute in tests
   (covered by plan 012 if it landed — skip that part if so).

## Current state

- `internal/tui/tui.go:270-277` (list mode `x` handler) and `:232-238`
  (confirm mode `s` handler):

  ```go
  case "x":
      if s, ok := m.current(); ok {
          m.selected = s
          m.killSingle = false
          m.plan = kill.Preview(s, m.killOpts())
          m.mode = modeConfirm
      }
  // ... in modeConfirm:
  case "s":
      if !m.plan.Docker && !m.plan.NoPID {
          m.killSingle = !m.killSingle
          m.plan = kill.Preview(m.selected, m.killOpts())   // second full snapshot
      }
  ```

- `internal/kill/kill.go` — `Preview(s, o)` → `previewWith(s, o, snapshot())`;
  `planTree(pid, single, tbl)` is pure given a table. After plan 001,
  `snapshot` is behind the `takeSnapshot` seam. `Opts.Single` is the only
  field the `s` toggle changes.
- `internal/classify/classify.go:45-57`:

  ```go
  func Process(servers []model.Server, cfg config.Config) {
      cache := project.NewCache()
      for i := range servers {
          if servers[i].Source == model.SourceDocker { continue }
          s := &servers[i]
          if s.Cwd != "" { s.Project = cache.Detect(s.Cwd) }
          s.Confidence = scoreProcess(*s, cfg)
      }
  }
  ```

- `internal/cli/collect.go`:

  ```go
  // collect gathers the merged native+docker server inventory.
  func collect(cfg config.Config) ([]model.Server, error) {
      return inventory.Collect(cfg)
  }
  ```

- `internal/cli/list.go:75-95` — `listOnce`: `--no-ignore` clears the lists
  on a value copy; hidden = `len(allView) - len(servers)` when `!all`.
- `internal/cli/config.go:19-41` — `--init` writes a default config but
  refuses when the file already exists.
- Config tests isolate via `XDG_CONFIG_HOME` pointed at a temp dir — see
  `internal/config/config_test.go` for the pattern.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Kill tests | `go test ./internal/kill/ -v` | pass |
| TUI tests | `go test ./internal/tui/ -v` | pass |
| Classify tests | `go test ./internal/classify/ -v` | pass |
| CLI tests | `go test ./internal/cli/ -v` | pass |
| Suite | `make lint && make test` | exit 0 |

## Scope

**In scope**:
- `internal/kill/kill.go` (add `PreviewBoth`; nothing else)
- `internal/kill/kill_test.go`
- `internal/tui/tui.go` (x/s handlers + a `previewBoth` seam field), `internal/tui/tui_test.go`
- `internal/classify/classify_test.go`
- `internal/cli/collect.go` (function → package var), `internal/cli/list_test.go`
  (extend/new), `internal/cli/config.go` (only if a writer needs injecting),
  a config-cmd test

**Out of scope**:
- Deleting `collect.go` — it becomes the seam (supersedes the audit's
  deletion suggestion; recorded in plans/README.md).
- `kill.Preview`/`PreviewBatch` signatures — unchanged.
- Moving the initial preview into an async `tea.Cmd` — considered, deferred
  (state-ordering complexity outweighs the win once the toggle re-snapshot is
  gone); note it in Maintenance.
- `scan.Processes` row-construction tests — landed with plan 005.

## Git workflow

- Branch: `advisor/013-test-seams`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: `kill.PreviewBoth` — one snapshot, both scopes

In `internal/kill/kill.go`:

```go
// PreviewBoth computes the whole-tree and listener-only Plans from a single
// process-table snapshot, so a confirmation UI can toggle scope without
// re-snapshotting.
func PreviewBoth(s model.Server, o Opts) (tree, single Plan) {
    if s.Source == model.SourceDocker {
        p := Plan{Server: s, Docker: true}
        return p, p
    }
    if s.PID <= 0 {
        p := Plan{Server: s, NoPID: true}
        return p, p
    }
    tbl := takeSnapshot()
    oo := o
    oo.Single = false
    tree = previewWith(s, oo, tbl)
    oo.Single = true
    single = previewWith(s, oo, tbl)
    return tree, single
}
```

Test (synthetic table via the plan-001 `takeSnapshot` seam): a launcher
parent with two children → `tree.Tree` has 3 members rooted at the launcher,
`single.Tree` has exactly the listening pid; docker/no-pid short-circuits
return matching flag pairs.

**Verify**: `go test ./internal/kill/ -v` → pass.

### Step 2: TUI uses PreviewBoth via a seam

In `internal/tui/tui.go`:

- Add a model field: `previewBoth func(pm.Server, kill.Opts) (kill.Plan, kill.Plan)`,
  defaulted in `New(...)` to `kill.PreviewBoth`.
- Add model fields `planTree, planSingle kill.Plan` (replacing the single
  `plan` field) OR keep `m.plan` and add `m.planAlt` — smallest clear diff:
  store both, and a helper `m.currentPlan()` returning the one matching
  `m.killSingle`.
- `x` handler: `m.planTree, m.planSingle = m.previewBoth(s, m.killOpts())`,
  `m.killSingle = false`, enter confirm.
- `s` handler: just flip `m.killSingle` (no preview call, no snapshot) when
  `!m.currentPlan().Docker && !m.currentPlan().NoPID`.
- `confirmView` and the `y` dispatch read `m.currentPlan()` /
  `m.killOpts()` exactly as before. `killOpts()` already derives `Single`
  from `m.killSingle` — the kill itself is unchanged.

Tests:
- Port the synthetic-tree scenario from `internal/kill/kill_test.go`
  ("make with siblings") into a TUI test: inject a fake `previewBoth`
  returning a 3-member tree plan; press `x`; assert `confirmView()` output
  contains all three PIDs and the `— 3 processes` header; press `s`; assert
  the view now shows exactly the single pid and `scope: listener only`; press
  `s` again → back to 3. Assert the fake was called exactly ONCE (the perf
  point).
- Update `TestConfirmPreviewsBlastRadius` (tui_test.go:87-109) to inject the
  fake instead of relying on real PID 100 — it becomes deterministic.

**Verify**: `go test ./internal/tui/ -v` → pass.

### Step 3: `classify.Process` orchestration test

In `internal/classify/classify_test.go`: build a slice of three servers —
(a) `Source: SourceDocker, Confidence: 80` pre-set, (b) native with
`Cwd` pointing at a `t.TempDir()` containing a `.git` directory and a
`package.json` with a name, (c) native with empty `Cwd`. Run
`Process(servers, cfg)` with a cfg whose DevRoots contains the temp dir's
parent. Assert: (a) untouched (project nil unless pre-set, confidence still
80 — the docker skip); (b) `Project` non-nil with the manifest name and
`Confidence > 0`; (c) `Project` nil but `Confidence` set (scored on cmd
heuristics alone).

**Verify**: `go test ./internal/classify/ -v` → pass.

### Step 4: CLI layer seam + tests

- `internal/cli/collect.go`: `func collect(...)` → `var collect = func(cfg config.Config) ([]model.Server, error) { return inventory.Collect(cfg) }`
  (keep the doc comment; callers unchanged).
- New tests in `internal/cli/list_test.go` swapping `collect` (restore via
  `t.Cleanup`) with fixtures:
  1. **Hidden count**: 3 servers, threshold hides 2 → `listOnce` returns 1
     server, `hidden == 2`; with `all: true` → 3 servers, `hidden == 0`.
  2. **--no-ignore**: cfg with `IgnorePorts: [5432]`, fixture holds port 5432
     → default: absent; `noIgnore: true`: present; AND the passed cfg value
     outside the call still has its ignore list (value-copy semantics).
- Config `--init` guard: with `XDG_CONFIG_HOME` at a fresh temp dir, run the
  init path once (file created), run again → the "already exists" refusal,
  file mtime/content unchanged. If the run-function writes to `os.Stdout`
  directly, assert on behavior (file state + returned error) rather than
  output text; only inject a writer if an assertion is impossible without it.

**Verify**: `go test ./internal/cli/ -v` → pass; `make lint && make test` →
exit 0.

## Test plan

Steps 1–4 are the test plan. Patterns: plan-001 seam swaps with `t.Cleanup`;
`XDG_CONFIG_HOME` isolation from `internal/config/config_test.go`; TUI
Update-pumping from `internal/tui/tui_test.go`.

## Done criteria

- [ ] `kill.PreviewBoth` exists with tests; `grep -n "kill.Preview(" internal/tui/tui.go` → no matches (both sites use the seam)
- [ ] TUI blast-radius test uses an injected preview (no real-process dependency); toggle test proves ONE preview call per confirm
- [ ] `classify.Process` docker-skip and empty-cwd tests pass
- [ ] `listOnce` hidden-count and `--no-ignore` tests pass; `config --init` guard test passes
- [ ] `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- Plan 001 not DONE.
- `tui.go` has drifted so far (plans 003/009) that the x/s excerpts don't
  match — reconcile from the live code only if the change is mechanical;
  otherwise report.
- The config init path cannot be tested without restructuring `newConfigCmd`
  beyond extracting a small run function — report with the blocker.

## Maintenance notes

- Deferred deliberately: making the `x`-press preview asynchronous
  (`tea.Cmd` + `planMsg`). With the toggle fixed, the remaining cost is one
  snapshot per confirm-open; if that's still felt on huge process tables,
  the async design needs care around confirm-mode state ordering.
- Plan 019's detail-view tree should reuse the `previewBoth`/seam machinery
  added here (its plan says so).
- The `collect` var is the CLI's one inventory seam — new CLI tests should
  swap it rather than invent parallel injection.
