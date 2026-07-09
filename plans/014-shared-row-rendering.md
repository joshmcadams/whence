# Plan 014: Single source of truth for row rendering and server descriptions (CLI + TUI)

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/output internal/cli/kill.go internal/tui/tui.go`
> Plan 003 (sanitization) must be DONE — this plan consolidates the render
> boundaries it touched and must keep the sanitizer applied. Plans 009/013
> may also have edited `tui.go`; reconcile excerpts against live code.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (display-only; existing tests pin the important substrings)
- **Depends on**: plans/003-terminal-escape-sanitization.md
- **Category**: tech-debt
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

The architecture guarantees CLI and TUI "can never diverge" — but that only
covers *filtering* (shared `inventory.View`). The *rendering* is two lockstep
copies that have already diverged twice:

1. Two same-purpose `describe()` functions: the CLI renders
   `:%d name [container %s]` / `:%d name [pid %d]`; the TUI renders
   `:%d name` (container name dropped!) / `:%d name (pid %d)`. The same kill
   is described differently depending on where you confirm it.
2. Two table-row recipes: `output.Table` and `tui.rebuild` build the same
   cells (name + `[!]` exposure marker, uptime, src, truncated description),
   but only the CLI has the `-` name fallback and the show-first-Note
   fallback — an unattributed row explains itself in `whence list` and shows
   a blank description in the TUI.

Every new column or marker currently must be added twice; one side gets
forgotten.

## Current state

(Line numbers are from `caec51a`; plans 003/009/013 may have shifted them —
the shapes are what matters.)

- `internal/cli/kill.go:197-208`:

  ```go
  func describe(s model.Server) string {
      name := s.DisplayName()
      if name == "" { name = "(unknown)" }
      switch s.Source {
      case model.SourceDocker:
          return fmt.Sprintf(":%d %s [container %s]", s.Port, name, s.Name)
      default:
          return fmt.Sprintf(":%d %s [pid %d]", s.Port, name, s.PID)
      }
  }
  ```

- `internal/tui/tui.go:486-495`:

  ```go
  func describe(s pm.Server) string {
      name := s.DisplayName()
      if name == "" { name = "(unknown)" }
      if s.Source == pm.SourceDocker {
          return fmt.Sprintf(":%d %s", s.Port, name)
      }
      return fmt.Sprintf(":%d %s (pid %d)", s.Port, name, s.PID)
  }
  ```

- `internal/output/output.go:49-67` — Table's row build: name with `-`
  fallback and `[!]` when `Exposure()=="all"`; desc falling back to
  `note(s)` (`(Notes[0])` or `-`); pid `-` when ≤ 0; `HumanUptime`;
  `SrcLabel`; `Truncate(desc, 60)`.
- `internal/tui/tui.go:311-330` — `rebuild()`: same recipe minus the name
  and note fallbacks; width-driven `Truncate(desc, descW)`.
- After plan 003, both paths route content through `output.Sanitize`.
- `internal/output` is already imported by both `cli` and `tui` — the shared
  home. `output` must not import `tui` or `cli` (dependency direction,
  AGENTS.md).
- Callers of the CLI `describe`: `runKill` output lines, `printPlan`. Callers
  of the TUI `describe`: confirm head, `killing …` status, `killedMsg`
  status.
- Tests pinning current strings: `internal/tui/tui_test.go` (status/confirm
  substrings), `internal/output/output_test.go` (table content),
  `internal/cli/kill_test.go` (plan-001 additions, if landed).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Output tests | `go test ./internal/output/ -v` | pass |
| CLI + TUI tests | `go test ./internal/cli/ ./internal/tui/ -v` | pass |
| Suite | `make lint && make test` | exit 0 |
| Manual | `make run ARGS="list --all"` and `make run ARGS=tui` | rows visually consistent |

## Scope

**In scope**:
- `internal/output/output.go`, `internal/output/output_test.go` (new shared
  `Describe` + `Row`)
- `internal/cli/kill.go` (delete local `describe`, use shared)
- `internal/tui/tui.go` (delete local `describe`, use shared; `rebuild` uses
  shared row cells)
- `internal/cli/kill_test.go`, `internal/tui/tui_test.go` (expectation updates
  where the unified wording deliberately changes)

**Out of scope**:
- Column layout/widths (tabwriter vs bubbles table stay as they are — the
  shared function returns CELLS, not layout).
- `output.JSON` — untouched.
- Any new column (plan 019 adds surface; not here).

## Git workflow

- Branch: `advisor/014-shared-rendering`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Shared `output.Describe`

Add to `internal/output/output.go`:

```go
// Describe is the one-line identity of a server used in kill confirmations
// and status lines — shared by CLI and TUI so the same kill is always
// described the same way.
func Describe(s model.Server) string {
    name := Sanitize(s.DisplayName())
    if name == "" { name = "(unknown)" }
    if s.Source == model.SourceDocker {
        return fmt.Sprintf(":%d %s [container %s]", s.Port, name, Sanitize(s.Name))
    }
    return fmt.Sprintf(":%d %s [pid %d]", s.Port, name, s.PID)
}
```

Unification decision (deliberate): the CLI's format wins — it carries the
container name / pid the TUI was dropping, and brackets are the existing CLI
style. The TUI's `(pid %d)` form disappears.

Table-test it: docker with name, native with pid, empty display name →
`(unknown)`, name containing an escape byte → sanitized.

### Step 2: Shared `output.Row`

```go
// Row builds the display cells for one server, shared by the CLI table and
// the TUI so the two renderings cannot drift: PORT, PROTO, PID, UPTIME, SRC,
// SERVER (with the [!] all-interfaces marker), DESCRIPTION (truncated to
// descWidth; falls back to the first Note).
func Row(s model.Server, descWidth int) []string
```

Fold in the superset of today's rules (this deliberately FIXES the TUI's
missing fallbacks): name `-` fallback + `[!]` marker; desc → first note in
parens → `-`; pid `-` when ≤ 0; `HumanUptime`; `SrcLabel`; sanitize name,
proto, desc/note. Return all seven cells; each renderer picks the columns it
shows (`output.Table` uses all seven; `tui.rebuild` uses PORT, PROTO, UPTIME,
SRC, SERVER, DESCRIPTION — indices, not re-derivation).

Rewrite `Table`'s loop and `tui.rebuild`'s loop to consume `Row`. In
`rebuild`, keep the width calculation (`descWidth(m.width)`) and pass it in;
`Table` keeps its fixed 60.

### Step 3: Replace both `describe` copies

Delete the local `describe` in `internal/cli/kill.go` and
`internal/tui/tui.go`; call `output.Describe` at every former call site.
Update tests that pinned the old TUI wording (`(pid %d)` → `[pid %d]`;
confirm-head/status lines now include `[container …]`). Every other pinned
substring should survive.

**Verify** after each step: `go test ./internal/output/ ./internal/cli/ ./internal/tui/ -v`
→ pass; finally `make lint && make test` → exit 0, plus the manual smoke
(`list --all` vs `tui` show the same cells for the same server; an
unattributed row now shows its note in BOTH).

## Test plan

- `output_test.go`: `TestDescribe` (4 cases above) and `TestRow` (attributed
  row, unattributed row with note, exposure-all marker, empty-everything
  fallbacks, truncation at descWidth).
- Existing CLI/TUI tests updated only where this plan's unification
  deliberately changed wording — list each such change in the final report.

## Done criteria

- [ ] `grep -rn "func describe" internal/cli internal/tui` → no matches
- [ ] `output.Describe` and `output.Row` exist with table tests
- [ ] TUI rows show note-fallback descriptions (test proves an unattributed
      server renders its note in the TUI table cells)
- [ ] Sanitizer still applied: `grep -n "Sanitize" internal/output/output.go` shows it inside `Describe`/`Row`
- [ ] `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- Plan 003 not DONE (the sanitizer must exist to be preserved).
- A test pins TUI wording that a HUMAN decision seems to protect (e.g. the
  shorter docker describe was deliberate for narrow terminals) — nothing in
  the repo says so, but if you find such a note, report instead of unifying.
- `output.Row`'s cell set can't serve both renderers without layout logic
  leaking into `output` — report the specific mismatch.

## Maintenance notes

- New columns/markers now land in `output.Row` once; reviewers should reject
  any future PR that re-adds per-renderer cell logic.
- Plan 019 (detail-view tree, sort key) and plan 022 (bubbletea v2) build on
  the post-consolidation `tui.go`; keeping `rebuild` thin makes both easier.
- The TUI gains slightly wider SERVER cells for docker rows (container name
  in describe contexts only, not in Row) — no column change.
