# Plan 022: Migrate the TUI stack to charmbracelet v2

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/tui internal/cli/tui.go go.mod`
> This plan runs LAST among the TUI plans (009, 013, 014, 017, 019 first) —
> verify their status rows before starting; migrating under them wastes both
> efforts.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED (core TUI API changes; the Update-pumping test suite is the net)
- **Depends on**: plans/009, 013, 014, 017, 019
- **Category**: migration
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

The TUI stack is a full major behind: as of 2026-07, `bubbletea/v2 v2.0.8`,
`bubbles/v2 v2.1.1`, and `lipgloss/v2 v2.0.5` are GA on new module paths,
and `go list -m -u` shows the v1 line no longer receiving updates — new
fixes land only on v2. The v1 tree also drags stale indirect deps that v2
dropped (`xo/terminfo` 2022, `erikgeiser/coninput` 2021, `muesli/ansi`
2023, plus three text-width libraries). No breakage today; this is scheduled
lag-clearing with a slowly compounding cost, done after the TUI churn above
has settled.

## Current state

- `go.mod:7-9` — `bubbles v1.0.0`, `bubbletea v1.3.10`, `lipgloss v1.1.0`.
- Charm imports exist in exactly four files (verified):
  - `internal/tui/tui.go` — `bubbles/table`, `bubbles/textinput`,
    `bubbletea`, `lipgloss` (post-017 also `bubbles/key` via
    `internal/tui/keys.go`)
  - `internal/tui/theme.go` — `lipgloss` (+ `bubbles/table` styles)
  - `internal/tui/tui_test.go` — `bubbletea` (KeyMsg construction,
    Update pumping)
  - `internal/cli/tui.go` — `tea.NewProgram(...)`
- The test suite drives `Update()` directly with synthetic messages —
  exactly the kind of behavior-level coverage that survives an API
  migration and catches its regressions.
- Known v2 change areas (verify against the official notes — do NOT trust
  this list as complete): key messages split (`tea.KeyPressMsg` /
  `KeyReleaseMsg` replacing plain `KeyMsg` in many paths), `Init`/program
  option signatures, lipgloss color/profile handling, bubbles component
  option/style APIs. The migration guide and release notes at
  github.com/charmbracelet/bubbletea (v2 releases) are the source of truth.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Fetch modules | `go get github.com/charmbracelet/bubbletea/v2@v2.0.8 github.com/charmbracelet/bubbles/v2@v2.1.1 github.com/charmbracelet/lipgloss/v2@v2.0.5` | go.mod updated |
| Tidy | `go mod tidy` | v1 charm modules gone from go.mod |
| Build | `go build ./...` | exit 0 |
| TUI tests | `go test ./internal/tui/ -v` | pass |
| Suite | `make lint && make test` | exit 0 |
| Manual | `make run ARGS=tui` | full keyboard walkthrough below |

## Scope

**In scope**:
- `go.mod` / `go.sum` (the three charm majors + resulting indirect changes)
- `internal/tui/tui.go`, `internal/tui/keys.go`, `internal/tui/theme.go`,
  `internal/tui/tui_test.go`, `internal/cli/tui.go`

**Out of scope**:
- Behavior changes of ANY kind — this is a mechanical migration; every test
  expectation stays (only construction/API syntax may change).
- Other dependency updates (`go mod tidy` fallout is fine; deliberate bumps
  are not).
- New v2-only features (adopt later, deliberately).

## Git workflow

- Branch: `advisor/022-charm-v2`
- Commit per step; the final commit message lists every API rename applied.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Read the migration surface

Fetch the modules (Commands table). Read the v2 release notes / migration
guide (`go doc github.com/charmbracelet/bubbletea/v2` plus the repo's
release notes if network allows). List, before editing anything, each API
this repo touches that changed — grep the four files for `tea.`, `table.`,
`textinput.`, `key.`, `lipgloss.` symbols and check each against v2 docs
(`go doc <pkg>.<Symbol>`).

**Verify**: the written-down rename list (include it in the final report).

### Step 2: Mechanical migration

Update the import paths to `/v2` in the four files (+ `keys.go`), apply the
rename list, `go mod tidy`. Common traps to check explicitly:

- `tui_test.go` key construction — whatever replaces
  `tea.KeyMsg{Type: ..., Runes: ...}` / `tea.KeyPressMsg` string forms; every
  existing test must send the v2 equivalent of the SAME keystrokes.
- `tea.NewProgram` options in `internal/cli/tui.go`.
- `table.New(...)`/`WithColumns`/`SetStyles` and `textinput.New()` shapes.
- lipgloss `Style` chaining used in `theme.go`/`tui.go` (mostly stable, but
  color/profile APIs moved).

**Verify**: `go build ./...` → exit 0; `go mod tidy && git diff go.mod` shows
no v1 charm modules remaining (`grep -c 'charmbracelet/[a-z]*  *v1' go.mod` → 0;
note `x/ansi`-style transitive charm libs may legitimately remain).

### Step 3: Test suite green

`go test ./internal/tui/ -v` — fix compilation/API fallout ONLY. Any test
that fails on *behavior* (not construction) is a real regression: fix the
migration, never the expectation. Then `make lint && make test`.

### Step 4: Manual walkthrough

`make run ARGS=tui`: arrows move; `/` filter types and Esc clears; `x` shows
the confirm box with tree + scope toggle `s`; `n` cancels; `t` cycles theme
AND persists (quit, relaunch, theme retained); `enter` detail view shows the
tree section; sort key cycles; `r` refreshes; `q` quits; resize the terminal
(columns reflow). Any visual/behavioral diff from pre-migration → STOP.

**Verify**: walkthrough checklist all pass (report each item).

## Test plan

No new tests. The existing behavior-level suite is the migration's net —
that's why this plan waits for 009/013/014/017/019, which thickened it.

## Done criteria

- [ ] go.mod has bubbletea/v2, bubbles/v2, lipgloss/v2 and no charm v1 majors
- [ ] Zero behavior-level test expectation changes (`git diff -- internal/tui/tui_test.go` shows construction-syntax changes only — state this in the report)
- [ ] `make lint && make test` exit 0
- [ ] Manual walkthrough checklist passes (theme persistence included)
- [ ] `plans/README.md` status row updated

## STOP conditions

- Any depended-on plan (009/013/014/017/019) is not DONE.
- A v2 API removal has no equivalent for something this TUI does (Step 1
  should surface it) — report the gap; do not redesign features mid-migration.
- Behavior test failures that survive a correct-looking migration — report
  with the failing case; that's a v2 semantic change worth a human decision.
- Module fetches fail (network) — report; nothing else in this plan is useful
  offline.

## Maintenance notes

- After this, `go list -m -u all` becomes meaningful again for the TUI stack;
  patch bumps within v2 are routine.
- Opportunistic follow-up (deferred from plan 017): splitting `view.go` out
  of `tui.go` — if the migration churn was total anyway, propose it as a
  separate immediately-after PR, not inside this one.
- Reviewer: diff scrutiny goes to `tui_test.go` — construction-only changes
  are the invariant that makes this diff trustable.
