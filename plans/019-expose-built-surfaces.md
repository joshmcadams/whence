# Plan 019: Expose built-but-hidden surfaces — detail-view tree, list query, TUI sort, multi-target kill

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. The four features are independent — if one hits a
> STOP condition, finish the others and report the blocked one. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat 65bc564..HEAD -- internal/cli internal/tui internal/inventory internal/kill README.md`
> Plans 013, 014, and 017 are DONE and have reshaped `tui.go` (test seams,
> shared rendering, keymap) and `cli/kill.go` (shared describe). Line
> numbers below reflect commit `65bc564`; reconcile against live code if
> drifted.

## Status

- **Priority**: P2
- **Effort**: M (four independent S features)
- **Risk**: LOW-MED (new surface, but every engine piece already exists and is tested)
- **Depends on**: plans/013, plans/017
- **Category**: direction
- **Planned at**: commit `65bc564`, 2026-07-09

## Why this matters

Four places where the machinery is built and tested but the user can't reach
it:

1. **Detail view lacks the promised process tree.** DESIGN.md's TUI spec
   promises `enter` → "detail view: full cmdline, cwd, README description,
   **child tree**". The tree is where a user judges whether a kill is safe —
   and `kill.Preview`/`Plan.Lines()` already compute and format exactly it.
2. **`inventory.View` has a query parameter the CLI never uses.** The TUI's
   `/` filter goes through it; `whence list` always passes `""`. There is no
   `whence list api` — you can *kill* by name but not *list* by name.
3. **The TUI can't sort.** `list --sort port|uptime|name` exists;
   `inventory.Sort` is shared; the TUI never calls it.
4. **`kill` takes exactly one target.** `dedupeUnits`, `kill.PreviewBatch`,
   and the execution loop are all plural already; `Args: cobra.ExactArgs(1)`
   is the only thing preventing `whence kill 3000 5173`.

## Current state

All line numbers from commit `65bc564` (post-plans 001-018).

- `internal/tui/tui.go:524-555` — `detailView` (post-014 shared rendering,
  post-017 inline help removed). Insertion point for the tree section is
  after the Description block (line 554), before `return b.String()`:

  ```go
  func (m Model) detailView() string {
      s := m.selected
      // ... Port/Bind/Server/Source rows ...
      // ... Container/Image or PID/Exe/Command/Cwd rows ...
      // ... Uptime/Confidence/Repo/Marker rows ...
      b.WriteString("\n" + detailLabel.Render("Description") + "\n")
      b.WriteString(wordWrap(output.Sanitize(s.Description()), 72) + "\n")
      // +++ TREE SECTION GOES HERE (Feature 1) +++
      return b.String()
  }
  ```
- `internal/tui/tui.go:41-73` — `Model` struct has `planTree` and
  `planSingle` (used by confirm flow) plus `previewBoth` seam (plan 013).
  `handleKey` line 360 (`enter` → detail mode) already sets `m.selected = s`.
- `internal/tui/keys.go:16-27` — `keyMap` struct (plan 017). List-mode
  bindings include: `Up, Down, Kill, Detail, Filter, All, Theme, Refresh,
  Quit`. No sort binding exists yet.
- `internal/tui/tui.go:440-470` — `helpLine()` has a `case modeList` block
  that emits list-mode footer help from bindings.
- `internal/cli/list.go:34` — `Args: cobra.NoArgs`. `listOnce` at line 82
  calls `inventory.View(raw, cfg, o.all, o.port, "")` at line 93 (query
  hardcoded to `""`) and a second `View(raw, cfg, true, o.port, "")` at
  line 98 for the hidden count.
- `internal/inventory/inventory.go:101` — `View` signature has `query
  string` parameter; `inventory.go:165` — `Sort(s []model.Server, by
  string)` exists.
- `internal/cli/kill.go:39-49` — `ExactArgs(1)`, `Use: "kill <port|name>"`,
  `RunE` calls `runKill(args[0], o)`. `matchTargets` at line 140 returns
  `([]model.Server, fuzzy bool)`. `dedupeUnits`, `confirmKill`, and the
  kill loop are all already plural (see `kill.go:101-122`).
- `internal/cli/kill.go:114,116` — kill output lines already use
  `output.Describe(s)` (plan 014).
- README: Usage section covers `list`/`kill`/`tui` commands; TUI key table
  at `README.md:86-97` covers list-mode keys only.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Suite | `make lint && make test` | exit 0 |
| Per-package | `go test ./internal/cli/ ./internal/tui/ ./internal/kill/ -v` | pass |
| Manual | `make run ARGS="list api"`, `make run ARGS=tui` | behave as specified |

## Scope

**In scope**:
- `internal/cli/list.go`, `internal/cli/kill.go` (+ their test files)
- `internal/tui/tui.go`, `internal/tui/keys.go`, `internal/tui/tui_test.go`
- `README.md` (Usage lines + TUI key row for the new abilities)

**Out of scope**:
- New `inventory`/`kill` engine capabilities — if a feature needs one, that's
  a STOP (the premise is wiring only). Exception: one tiny accessor noted in
  Feature 1.
- `whence config --edit` (plan 020), podman (plan 021).
- Changing existing sort/query/kill semantics.

## Git workflow

- Branch: `advisor/019-expose-surfaces`
- One commit per feature.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Feature 1: Process tree in the TUI detail view

- In `handleKey` (line 360), alongside the existing `m.selected = s`, compute
  the kill tree: `tree, _ := m.previewBoth(s, m.killOpts())`. Store in a
  **dedicated `m.detailPlan` field** — do NOT reuse `m.planTree` (it's
  clobbered when the user presses `x` to confirm-kill from detail mode).
  Add `detailPlan kill.Plan` to `Model` struct and `New()`.
- In `detailView`, after the Description block (line 554, before
  `return b.String()`), add a `Tree` section for attributed native servers:
  render `plan.Lines()` (sanitized — `output.Sanitize`) with the same
  12-line cap + `… +N more` overflow the confirm box uses
  (`maxConfirmTreeLines` at line 482). Extract the capped-render logic
  from `confirmView` (lines 498-508) into a shared helper inside tui:
  `func renderTreeLines(plan kill.Plan) []string`. For docker/no-PID
  servers, `Plan.Lines()` already returns a single explanatory line
  (`docker stop` or "no accessible pid").
- Test (Update-pumping + injected `previewBoth` fake): enter detail on a
  server whose fake plan has 3 members → `detailView()` output contains
  all three PIDs; docker server → contains `docker stop`.

**Verify**: `go test ./internal/tui/ -v` → pass.

### Feature 2: `whence list [query]`

- `newListCmd`: `Args: cobra.MaximumNArgs(1)`; thread `args[0]` (if present)
  into `listOnce` → `inventory.View(raw, cfg, o.all, o.port, query)`.
  The default `whence` (no subcommand → `runList`) keeps NoArgs behavior —
  check how root dispatch passes args (`internal/cli/root.go`) and keep
  `whence someword` NOT becoming a query at the root level (cobra will try
  to resolve `someword` as a subcommand and error — that existing behavior
  stays; only explicit `whence list someword` gains meaning).
- Both table and JSON paths get the filter (it's inside View). The hidden
  count (`listOnce`) computes `allView` with the SAME query so the "N hidden"
  hint stays truthful — pass the query to both View calls.
- Update the command Short/Long and README Usage
  (`whence list api  # only servers matching "api"`).
- Tests (via the plan-013 `collect` seam): fixture of two named servers;
  `list web` shows one; empty query shows both; hidden-count remains
  consistent under a query.

**Verify**: `go test ./internal/cli/ -v` → pass; manual
`make run ARGS="list nosuchthing"` → "No servers matched…" hint path.

### Feature 3: TUI sort key

- Add `Sort` binding to `defaultKeyMap()` in `keys.go`:
  `Sort: bkey.NewBinding(bkey.WithKeys("s"), bkey.WithHelp("s", "sort"))`.
  Add `Sort bkey.Binding` to the `keyMap` struct (list-mode section).
- In `helpLine()` `case modeList` (line 460): add `add(m.keys.Sort)` after
  the Theme line (before Refresh).
- In `handleKey` list-mode switch (line 398 area): add
  `case bkey.Matches(msg, m.keys.Sort):` that cycles the sort key.
  The `s` key is not used in list mode today; confirm mode's `s` (scope
  toggle) uses a `strings.ToLower` string switch, so no conflict.
- Model field `sortBy string` (default `"port"`); on `s` press:
  cycle `"port"` → `"uptime"` → `"name"` → `"port"`,
  set `m.status = "sort: " + key`, `m.rebuild()`.
- In `rebuild`, after `inventory.View(...)` (which sorts by port), apply
  `inventory.Sort(m.rows, m.sortBy)` when `m.sortBy != "port"`.
  Cursor stays at its index (same as refresh today); plan 012's stable sort
  keeps order deterministic.
- Header: append the sort key to the meta line when non-default
  (`· sort:uptime` in `headerView` at line 455) so state is visible.
- Tests: three fixture servers with distinct uptimes/names; press `s` →
  rows reorder (assert first row); cycle through all three keys; wrap.
- README TUI key table: add `s` row (`sort`).

**Verify**: `go test ./internal/tui/ -v` → pass.

### Feature 4: Multi-target kill

- `newKillCmd` (line 39): change `Use: "kill <port|name>"` to
  `"kill <port|name> [more...]"` and `cobra.ExactArgs(1)` to
  `cobra.MinimumNArgs(1)`.
- `RunE` becomes `runKill(args, o)` (accept `[]string`, not single string).
- `runKill(targets []string, o)` inside `runKillWith`: loop `matchTargets`
  per target; error `no server found matching %q` if ANY target matches
  nothing (all-or-nothing keeps behavior predictable — state this in the
  Long help). Union the matches; `fuzzy = true` if any target matched
  fuzzily. Then the existing `dedupeUnits` → `confirmKill` → kill loop,
  unchanged. The `--single` warning at line ~74 already keys off
  `len(units) > 1`.
- Confirmation wording: update `About to kill %d target(s) matching %q`
  in `confirmKill` (line ~219) to use `strings.Join(targets, ", ")` for
  the match list.
- Tests (plan-001 harness): `kill 3000 5173` with fixtures on both ports →
  one combined confirmation, both killed; one bogus target among two →
  error before any prompt; duplicate targets (`3000 3000`) → deduped to
  one unit.
- README Usage: `whence kill 3000 5173  # kill several at once`.

**Verify**: `go test ./internal/cli/ -v` → pass; `make lint && make test` →
exit 0.

## Test plan

Per-feature tests above; patterns: Update-pumping (tui_test.go), the
runKill/collect seams (plans 001/013).

## Done criteria

- [ ] Detail view shows the kill tree (test proves 3 fake PIDs render); DESIGN.md's promise is now true
- [ ] `whence list <query>` filters via the existing View param (test + manual)
- [ ] TUI `s` cycles sort with visible state (tests assert reorder)
- [ ] `whence kill a b` works with one confirmation; all-or-nothing matching tested
- [ ] README documents all four; `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- Plan 013's seam is absent (dependency not landed).
- Feature 2: root-level `whence <word>` dispatch would change behavior —
  report what cobra actually does before wiring anything at the root.
- Feature 4: unioning matches across targets breaks a `matchTargets` test
  expectation — the per-target function must stay untouched; only the caller
  loops.
- Any feature needs an engine change beyond Feature 1's noted reuse.

## Maintenance notes

- These four close the audit's surface asymmetries; the remaining
  DESIGN.md-promise gap is `config --edit` (plan 020).
- Feature 3's sort state is session-only by design (not persisted to config);
  if users ask for persistence, follow the theme-persistence pattern
  (`persistThemeCmd`).
- Reviewer: Feature 4's all-or-nothing rule is a UX decision — confirm you
  agree before merge.
