# Plan 014: Single source of truth for row rendering and server descriptions (CLI + TUI)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 9b41421..HEAD -- internal/output internal/cli/kill.go internal/tui/tui.go`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (display-only; existing tests pin substrings, not exact format)
- **Depends on**: plans/003-terminal-escape-sanitization.md
- **Category**: tech-debt
- **Planned at**: commit `9b41421`, 2026-07-09

## Why this matters

The architecture guarantees CLI and TUI "can never diverge" — but that only
covers *filtering* (shared `inventory.View`). The *rendering* is two lockstep
copies that have already diverged:

1. Two same-purpose `describe()` functions: the CLI renders
   `:%d name [container %s]` / `:%d name [pid %d]`; the TUI renders
   `:%d name` (container name dropped) / `:%d name (pid %d)`. The same kill
   is described differently depending on where you confirm it.
2. Two table-row recipes: `output.Table` and `tui.rebuild` build the same
   cells (name + `[!]` exposure marker, uptime, src, truncated description),
   but only the CLI has the `-` name fallback and the show-first-Note
   fallback — an unattributed row explains itself in `whence list` and shows
   a blank description in the TUI.

Every new column or marker currently must be added twice; one side gets
forgotten.

## Current state

All excerpts below are from commit `9b41421` (post-plans 003/005/009/013).

- `internal/cli/kill.go:225-236` — the CLI's `describe`:

  ```go
  // describe renders a server for a confirmation/status line. The name and
  // container/process identifiers can embed process- or repo-controlled text,
  // so they're sanitized here — every caller gets a terminal-safe string.
  func describe(s model.Server) string {
      name := output.Sanitize(s.DisplayName())
      if name == "" {
          name = "(unknown)"
      }
      switch s.Source {
      case model.SourceDocker:
          return fmt.Sprintf(":%d %s [container %s]", s.Port, name, output.Sanitize(s.Name))
      default:
          return fmt.Sprintf(":%d %s [pid %d]", s.Port, name, s.PID)
      }
  }
  ```

- `internal/tui/tui.go:556-565` — the TUI's `describe`:

  ```go
  // describe renders a server for a status/confirmation line. name comes from
  // scan/docker/project data and can embed process-controlled text, so it's
  // sanitized here — every caller gets a terminal-safe string.
  func describe(s pm.Server) string {
      name := output.Sanitize(s.DisplayName())
      if name == "" {
          name = "(unknown)"
      }
      if s.Source == pm.SourceDocker {
          return fmt.Sprintf(":%d %s", s.Port, name)
      }
      return fmt.Sprintf(":%d %s (pid %d)", s.Port, name, s.PID)
  }
  ```

- `internal/output/output.go:50-68` — `Table`'s row build (CLI):

  ```go
  for _, s := range servers {
      name := Sanitize(s.DisplayName())
      if name == "" {
          name = "-"
      }
      if s.Exposure() == "all" {
          name += " [!]"
      }
      desc := Sanitize(s.Description())
      if desc == "" {
          desc = note(s)
      }
      pid := "-"
      if s.PID > 0 {
          pid = fmt.Sprint(s.PID)
      }
      fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
          s.Port, Sanitize(s.Proto), pid, HumanUptime(s.Uptime), SrcLabel(s.Source), name, Truncate(desc, 60))
  }
  ```

- `internal/tui/tui.go:368-387` — `rebuild()`'s row build (TUI):

  ```go
  func (m *Model) rebuild() {
      m.rows = inventory.View(m.raw, m.cfg, m.all, 0, m.query)
      descW := descWidth(m.width)
      rows := make([]table.Row, len(m.rows))
      for i, s := range m.rows {
          name := output.Sanitize(s.DisplayName())
          if s.Exposure() == "all" {
              name += " [!]"
          }
          rows[i] = table.Row{
              fmt.Sprintf("%d", s.Port),
              output.Sanitize(s.Proto),
              output.HumanUptime(s.Uptime),
              output.SrcLabel(s.Source),
              name,
              output.Truncate(output.Sanitize(s.Description()), descW),
          }
      }
      m.table.SetRows(rows)
  }
  ```

  Note: the TUI's `rebuild` has *no* `-` name fallback and *no* note-fallback
  for the description — those are the bugs this plan fixes.

- `internal/output/output.go:72-77` — the private `note()` helper that both
  `Table` (and the new `Row`) use for description fallback:

  ```go
  func note(s model.Server) string {
      if len(s.Notes) > 0 {
          return "(" + Sanitize(s.Notes[0]) + ")"
      }
      return "-"
  }
  ```

- `internal/output` is already imported by both `cli` and `tui`. `output` must
  not import `tui` or `cli` (dependency direction, AGENTS.md).
- `output.Sanitize` exists and is called at every render boundary (plan 003).

**Callers of CLI `describe`**: `runKill` output lines (`kill.go:114,116`),
`printPlan` (`kill.go:216`).

**Callers of TUI `describe`**: `killedMsg` status (`tui.go:231,233`),
`killing …` status (`tui.go:285`), confirm head (`tui.go:462`).

**TUI column layout** (from `columns()` at `tui.go:529-538`): PORT, PROTO,
UPTIME, SRC, SERVER, DESCRIPTION — six columns, no PID. The CLI table has
seven: PORT, PROTO, PID, UPTIME, SRC, SERVER, DESCRIPTION. `Row` returns all
seven; the TUI skips PID.

**Affected tests** (see Test plan below for which change and how):

| Test (file:line) | Impact |
|---|---|
| `TestDescribe` (`cli/kill_test.go:337`) | Compile-break: calls local `describe()` — must move to output package |
| `TestTable_RendersRow` (`output/output_test.go:91`) | Output string changes if port format goes `%d`→`%s` |
| `TestTable_SanitizesEscapes` (`output/output_test.go:134`) | Unchanged (same sanitization path) |
| `TestRebuildSanitizesRowContent` (`tui/tui_test.go:250`) | TUI rows now have note-fallback in desc column — assertion may widen; check |
| `TestConfirmYesDispatchesKill` (`tui/tui_test.go:285`) | Substring `"killing"` still matches after unification |
| `TestKilledMsgSuccessSetsStatus` (`tui/tui_test.go:303`) | Substring `"✓ killed"` still matches |
| `TestKilledMsgErrorSetsStatus` (`tui/tui_test.go:316`) | Substring `"✗"` and error text still match |
| `TestConfirmPreviewsBlastRadius` (`tui/tui_test.go:108`) | Confirm head now includes `[pid %d]` — `strings.Contains(v, "100")` still matches |
| `TestDetailViewShowsBind` (`tui/tui_test.go:179`) | Unchanged (detail view doesn't use `describe`) |

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Output tests | `go test ./internal/output/ -v` | pass |
| CLI tests | `go test ./internal/cli/ -v` | pass |
| TUI tests | `go test ./internal/tui/ -v` | pass |
| Suite | `make lint && make test` | exit 0 |
| Manual smoke | `make run ARGS="list --all"` and `make run ARGS=tui` | 6 overlapping columns match; unattributed row shows its note in the TUI |

## Scope

**In scope**:
- `internal/output/output.go`, `internal/output/output_test.go` (new shared
  `Describe` + `Row`)
- `internal/cli/kill.go` (delete local `describe`, use `output.Describe`)
- `internal/cli/kill_test.go` (move `TestDescribe` to output package)
- `internal/tui/tui.go` (delete local `describe`, use `output.Describe`;
  `rebuild` uses `output.Row` cells)

**Out of scope**:
- Column layout/widths (tabwriter vs bubbles table stay as they are — `Row`
  returns CELLS, not layout).
- `output.JSON` — untouched.
- `internal/tui/tui_test.go` — no changes expected (all affected tests use
  substring matching that survives the format change; see table above).
- Any new column (plan 019 adds surface; not here).
- `internal/cli/collect.go`, `internal/cli/list.go` — describe/Row do not
  touch them.

## Git workflow

- Branch: `advisor/014-shared-rendering`
- Commit per step.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add shared `output.Describe`

Add to `internal/output/output.go`:

```go
// Describe is the one-line identity of a server used in kill confirmations
// and status lines — shared by CLI and TUI so the same kill is always
// described the same way.
func Describe(s model.Server) string {
    name := Sanitize(s.DisplayName())
    if name == "" {
        name = "(unknown)"
    }
    if s.Source == model.SourceDocker {
        return fmt.Sprintf(":%d %s [container %s]", s.Port, name, Sanitize(s.Name))
    }
    return fmt.Sprintf(":%d %s [pid %d]", s.Port, name, s.PID)
}
```

Unification decision (deliberate): the CLI's format wins — it carries the
container name / pid the TUI was dropping, and brackets are the existing CLI
style. The TUI's `(pid %d)` form disappears.

Add `TestDescribe` to `internal/output/output_test.go` with these cases:

1. Docker source: `{Port: 3000, Source: SourceDocker, Name: "web-1"}` → `":3000 web-1 [container web-1]"`
2. Process source: `{Port: 4000, Source: SourceProcess, PID: 42, Name: "node"}` → `":4000 node [pid 42]"`
3. Empty display name: `{Port: 5000, Source: SourceProcess, PID: 7}` → `":5000 (unknown) [pid 7]"`
4. Name with escape byte: `{Port: 6000, Source: SourceProcess, PID: 1, Name: "\x1b[8mhidden"}` → `":6000 ?[8mhidden [pid 1]"` (ESC → `?`)

Pattern: follow `TestSrcLabel` (`output_test.go:57`) — one test function,
table of cases, `t.Run`.

**Verify**: `go test ./internal/output/ -run TestDescribe -v` → all 4 cases pass.

### Step 2: Add shared `output.Row` and rewrite both consumers

Add to `internal/output/output.go`:

```go
// Row builds the seven display cells for one server, shared by the CLI
// table and the TUI so the two renderings cannot drift. Each renderer picks
// the columns it needs by index (see the constant list below).
//
// Index  Cell
//   0    PORT       e.g. "5173"
//   1    PROTO      e.g. "tcp" (sanitized)
//   2    PID        e.g. "100" or "-" when ≤ 0
//   3    UPTIME     e.g. "45s" (HumanUptime)
//   4    SRC        e.g. "proc" / "docker" (SrcLabel)
//   5    SERVER     DisplayName with " [!]" when Exposure()=="all", "-" when empty
//   6    DESCRIPTION Truncated to descWidth; falls back to note(s); "-" when empty
func Row(s model.Server, descWidth int) []string {
    name := Sanitize(s.DisplayName())
    if name == "" {
        name = "-"
    }
    if s.Exposure() == "all" {
        name += " [!]"
    }

    desc := Sanitize(s.Description())
    if desc == "" {
        desc = note(s)
    }

    pid := "-"
    if s.PID > 0 {
        pid = fmt.Sprint(s.PID)
    }

    return []string{
        fmt.Sprint(s.Port),
        Sanitize(s.Proto),
        pid,
        HumanUptime(s.Uptime),
        SrcLabel(s.Source),
        name,
        Truncate(desc, descWidth),
    }
}
```

Note this calls the existing private `note()` helper (same package). The
`Port` cell is now `fmt.Sprint` instead of `%d` — the tabwriter format string
in `Table` must change from `"%d\t%s\t..."` to `"%s\t%s\t..."` (all `%s`).

Now rewrite the two consumers:

**`Table` in `output.go`**: replace the `for _, s := range servers` loop body
(lines 51-67) with:

```go
for _, s := range servers {
    cells := Row(s, 60)
    fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
        cells[0], cells[1], cells[2], cells[3], cells[4], cells[5], cells[6])
}
```

**`tui.rebuild()` in `tui.go`**: replace the loop body (lines 370-384) with:

```go
for i, s := range m.rows {
    cells := output.Row(s, descW)
    rows[i] = table.Row{
        cells[0], // PORT
        cells[1], // PROTO
        cells[3], // UPTIME  (skip index 2 = PID)
        cells[4], // SRC
        cells[5], // SERVER
        cells[6], // DESCRIPTION
    }
}
```

The `columns()` function at `tui.go:529` is unchanged — the header widths are
already correct.

**Verify**: `go test ./internal/output/ ./internal/tui/ -v` → all pass.
Check `go build ./...` → exit 0.

### Step 3: Replace both `describe` copies

**`internal/cli/kill.go`**:

1. Delete the `describe` function (lines 222-236).
2. Replace every call `describe(s)` with `output.Describe(s)` at:
   - line 114: `fmt.Fprintf(d.out, "✗ %s — %v\n", output.Describe(s), res.Err)`
   - line 116: `fmt.Fprintf(d.out, "✓ killed %s (%s)\n", output.Describe(s), res.Method)`
   - line 216: `fmt.Fprintf(out, "  %s\n", output.Describe(p.Server))`

**`internal/tui/tui.go`**:

1. Delete the `describe` function (lines 553-565).
2. Replace every call `describe(...)` with `output.Describe(...)` at:
   - line 231: `output.Describe(msg.res.Server)`
   - line 233: `output.Describe(msg.res.Server)`
   - line 285: `output.Describe(m.selected)`
   - line 462: `output.Describe(m.selected)`

**`internal/cli/kill_test.go`**:

3. Delete the `TestDescribe` function (lines 337-353) — it calls the
   now-deleted package-level `describe`. Its 3 test cases are already
   covered in Step 1's new `output.TestDescribe` plus the sanitization case.
   The existing cases port identically (same inputs, same expected strings).

**Verify**: `go test ./internal/output/ ./internal/cli/ ./internal/tui/ -v` → all pass.
Then `make lint && make test` → exit 0.

### Step 4: Manual smoke

```sh
make run ARGS="list --all"   # rows render; unattributed row shows note in DESCRIPTION
make run ARGS=tui            # TUI opens; same 6 columns match CLI; select a docker
                             # server, press x — confirm head includes [container …];
                             # status/killed messages include it too
```

The manual smoke confirms: (1) both surfaces work, (2) an unattributed row now
shows its first Note in the TUI's DESCRIPTION column (the bug this plan fixes),
and (3) the docker container name appears in TUI kill status lines.

## Test plan

- `output_test.go`: new `TestDescribe` (4 cases per Step 1) and new `TestRow`
  — cases: attributed row, unattributed row with note, exposure-all marker,
  empty-everything fallbacks, truncation at descWidth.
  Pattern: `TestSrcLabel` (table-driven, `t.Run`).
- `cli/kill_test.go`: delete `TestDescribe` (cases moved to output).
- `tui/tui_test.go`: no changes needed — all affected tests use substring
  matching (`"killing"`, `"✓ killed"`, `"✗"`, `"100"`) that survives the
  `(pid %d)` → `[pid %d]` and container-name-addition changes. If any test
  assertion fails that is NOT listed in the "Affected tests" table above,
  treat it as a STOP condition.

## Done criteria

- [ ] `grep -rn "func describe(" internal/cli/kill.go internal/tui/tui.go` → no matches
- [ ] `output.Describe` exists with `TestDescribe` (4 cases passing)
- [ ] `output.Row` exists with `TestRow` (≥5 cases passing)
- [ ] `grep -n "Sanitize" internal/output/output.go` shows it inside `Describe` and `Row`
- [ ] TUI DESCRIPION column now shows the first Note for an unattributed row
      (proven by the `TestRow` unattributed-with-note case)
- [ ] `make lint && make test` exit 0
- [ ] `go test ./internal/cli/ -v` → pass (confirming `TestDescribe` removal didn't leave dead imports)
- [ ] `plans/README.md` status row updated

## STOP conditions

- Plan 003 not DONE (the sanitizer must exist to be preserved). Verify:
  `grep -q "func Sanitize" internal/output/output.go` → exit 0.
- The code at the locations in "Current state" doesn't match the excerpts
  (the codebase has drifted since this plan was written at `9b41421`).
- A step's compiler or test failure persists after two reasonable fix
  attempts.
- The fix appears to require touching the `columns()` function or changing
  any column width — that means layout is leaking into your implementation;
  report the specific conflict.
- Any TUI test assertion fails that was NOT listed in the "Affected tests"
  table above — the unification changed a behavior the tests don't expect.

## Maintenance notes

- New columns/markers now land in `output.Row` once; reviewers should reject
  any future PR that re-adds per-renderer cell logic.
- Plan 019 (detail-view tree, sort key) and plan 022 (bubbletea v2) build on
  this plan's post-consolidation `tui.go`; `rebuild` is now trivial, making
  both migrations simpler.
- The kill confirmation/status lines now include the container name for docker
  servers — this is `Describe` only, not the table SERVER column, so column
  widths are unchanged.
- `output.Row`'s index 2 (PID) is the only cell the TUI doesn't render. If
  the TUI ever adds a PID column to its table header, add `cells[2]` at the
  right position in `rebuild`'s `table.Row` constructor.
