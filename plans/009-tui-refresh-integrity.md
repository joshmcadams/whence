# Plan 009: TUI refresh integrity — in-flight guard + snapshot generation counter

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/tui`
> On drift, compare excerpts below to live code before proceeding.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (additive model state; worst regression is a skipped refresh frame)
- **Depends on**: none (but touches `internal/tui/tui.go` — serialize with plans 003/013/014/017/019/022 in index order)
- **Category**: bug + perf
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Every 5s tick unconditionally spawns a new `inventory.Collect`, with no check
that the previous one finished. A `Collect` can legitimately take ~10s when
the docker daemon is slow (two sequential 5s execx timeouts inside
`docker.Servers`), so collects stack without bound for as long as the TUI is
open — each one a socket scan plus docker subprocesses. Worse, `loadedMsg`
unconditionally overwrites the table, so a SLOW OLD snapshot arriving after a
FAST NEW one rolls the view back: a just-killed server can reappear until the
next tick, inviting a confused second kill.

## Current state

`internal/tui/tui.go`:

```go
// tui.go:167-172
case tickMsg:
    if m.mode == modeFilter {
        return m, tickCmd()
    }
    return m, tea.Batch(loadCmd(m.cfg), tickCmd())

// tui.go:161-165
case loadedMsg:
    m.err = msg.err
    m.raw = msg.servers
    m.rebuild()
    return m, nil

// tui.go:121-126
func loadCmd(cfg config.Config) tea.Cmd {
    return func() tea.Msg {
        s, err := inventory.Collect(cfg)
        return loadedMsg{servers: s, err: err}
    }
}

// tui.go:102-105
type loadedMsg struct {
    servers []pm.Server
    err     error
}
```

Other `loadCmd` call sites: `Init()` (tui.go:148), the `killedMsg` handler
(tui.go:180, immediate refresh after a kill), and the `r` key (tui.go:258).
The user-triggered ones should be allowed to run even when a tick-load is in
flight — they just need the generation stamp so late arrivals can't clobber
newer state.

Tests: `internal/tui/tui_test.go` pumps `Update()` directly with synthetic
messages and discards returned commands via a `step()` helper — match that
style. `refreshInterval` is `5 * time.Second` (tui.go:23).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| TUI tests | `go test ./internal/tui/ -v` | pass |
| Full suite | `make test` | ok |
| Lint | `make lint` | exit 0 |
| Manual smoke | `make run ARGS=tui` (quit with `q`) | table renders, refreshes |

## Scope

**In scope**:
- `internal/tui/tui.go`
- `internal/tui/tui_test.go`

**Out of scope**:
- `inventory.Collect`, `internal/docker` timeouts — unchanged.
- The `refreshInterval` value.
- The synchronous `kill.Preview` on keypress (plan 013/015 territory — see
  index; do not drive-by it here).

## Git workflow

- Branch: `advisor/009-tui-refresh-guard`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Generation-stamped loads

- Add to `Model`: `loadSeq int` (last issued), `appliedSeq int` (last applied),
  `loading bool`.
- Change `loadedMsg` to carry `seq int`.
- Replace `loadCmd(cfg)` with a method `(m *Model) nextLoadCmd() tea.Cmd`
  that increments `m.loadSeq`, captures it, sets `m.loading = true`, and
  returns the command producing `loadedMsg{seq: captured, ...}`.
  NOTE Bubble Tea value semantics: `Update` receives `Model` by value and
  returns it — mutate the copy then return it, exactly as the existing
  handlers do; the helper must be written so the mutation lands on the
  returned model (easiest: make it `func (m Model) nextLoadCmd() (Model, tea.Cmd)`).
- `loadedMsg` handler: if `msg.seq <= m.appliedSeq`, ignore the message
  entirely (return `m, nil`); otherwise set `m.appliedSeq = msg.seq`, clear
  `m.loading` only when `msg.seq == m.loadSeq` (an older-but-newest-applied
  message keeps `loading` true while a newer one is still out), then apply
  as today.

### Step 2: Tick guard

In the `tickMsg` handler: when `m.loading` is true (or mode is filter, as
today), return `m, tickCmd()` without issuing a load. `Init`, `killedMsg`,
and `r` keep issuing loads unconditionally — through `nextLoadCmd` so they're
stamped.

### Step 3: Tests

Match the existing Update-pumping style:

1. **Stale drop**: issue two loads (seq 1, 2 via the real helper), deliver
   `loadedMsg{seq:2, servers: [A,B]}` then `loadedMsg{seq:1, servers: [C]}` →
   rows still reflect `[A,B]`.
2. **Tick skip while loading**: model with `loading == true` receives
   `tickMsg` → returned command produces only a `tickMsg` (no `loadedMsg`).
   Practical assertion without executing async commands: expose the decision
   via the model — after pumping, `m.loadSeq` unchanged.
3. **Tick loads when idle**: `loading == false`, `tickMsg` → `m.loadSeq`
   incremented.
4. **Manual refresh always loads**: `loading == true`, key `r` → `m.loadSeq`
   incremented (user intent wins).
5. Existing tests pass unchanged.

**Verify**: `go test ./internal/tui/ -v` → all pass;
`make lint && make test` → exit 0; manual `make run ARGS=tui` smoke: rows
appear, `r` works, `q` quits.

## Test plan

Step 3 is the test plan; pattern: `internal/tui/tui_test.go`'s synthetic
message pumping.

## Done criteria

- [ ] `loadedMsg` carries a sequence number; the stale-drop test passes
- [ ] `tickMsg` issues no load while one is in flight (test proves via loadSeq)
- [ ] `r` and post-kill refresh still always load
- [ ] All pre-existing TUI tests pass unchanged
- [ ] `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- The value-semantics refactor of `loadCmd` forces signature changes outside
  `internal/tui` — it must not; report.
- Any pre-existing TUI test needs a semantic (not mechanical) change.
- The manual smoke test shows the table never populating (guard wedged shut —
  check that `loading` is cleared on the applied path).

## Maintenance notes

- If a future watchdog is wanted (a load that never returns keeps `loading`
  true and stops ticking forever), note that `inventory.Collect` is bounded
  by scan syscalls + two 5s docker timeouts, so it always returns; the guard
  cannot wedge permanently. If docker timeouts are ever raised above
  `refreshInterval`×N, revisit.
- Plans 017/022 restructure key handling / migrate bubbletea; the seq fields
  are plain model state and should survive both — reviewers of those plans
  should re-run the stale-drop test.
