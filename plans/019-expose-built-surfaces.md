# Plan 019: Expose built-but-hidden surfaces — detail-view tree, list query, TUI sort, multi-target kill

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. The four features are independent — if one hits a
> STOP condition, finish the others and report the blocked one. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/cli internal/tui internal/inventory internal/kill README.md`
> Plans 009/013/014/017 land before this one and reshape `tui.go` (seams,
> shared rendering, keymap); build on their landed state.

## Status

- **Priority**: P2
- **Effort**: M (four independent S features)
- **Risk**: LOW-MED (new surface, but every engine piece already exists and is tested)
- **Depends on**: plans/013-remaining-test-seams.md (preview seam); plans/017 preferred first (keymap) but not required
- **Category**: direction
- **Planned at**: commit `caec51a`, 2026-07-09

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

- `internal/tui/tui.go` `detailView` (shape at `caec51a`, lines 425-458):
  renders Port/Bind/Server/Source/…/Description rows; no tree. After plan
  013, the model has a `previewBoth` seam and `planTree`/`planSingle` fields
  used by the confirm flow.
- `internal/cli/list.go:33` — `Args: cobra.NoArgs`; `list.go:86` —
  `inventory.View(raw, cfg, o.all, o.port, "")`. `View`'s query matching
  (`inventory.go:107-115`) covers name/description/port-digits, tested.
- `internal/inventory/inventory.go:78` — `View` always sorts by `"port"`;
  `listOnce` re-sorts by the user key after View.
- `internal/cli/kill.go:34` — `ExactArgs(1)`; `matchTargets` (name-or-port),
  `dedupeUnits` (PID/container dedupe), `confirmKill` (batch preview +
  count), the loop at `kill.go:84-93` aggregating failures — all plural.
  `--single` warning at `kill.go:64-68` keys off `len(units) > 1`.
- Keymap (post-017): bindings in `internal/tui/keys.go`; footer help renders
  from them. Free keys: `s` is taken in confirm mode only — usable in list
  mode for sort; `o` also free (pick `s` for "sort"; no conflict since modes
  are disjoint switches).
- README Usage + TUI key table document the current surface (updated by
  plan 008; this plan extends both).

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

- On `enter` (list mode), alongside `m.selected = s`, compute the tree plan:
  reuse the plan-013 seam — `tree, _ := m.previewBoth(s, m.killOpts())` —
  and store it (reuse `m.planTree` or a dedicated `m.detailPlan`; pick
  whichever keeps confirm-flow state untouched).
- In `detailView`, after the Cwd/Description rows, add a `Tree` section for
  native attributed servers: render `plan.Lines()` (sanitized —
  `output.Sanitize` per plan 003) with the same 12-line cap +
  `… +N more` overflow the confirm box uses (`maxConfirmTreeLines` — reuse
  the constant and the capped-render logic; extract a small shared helper
  inside tui rather than duplicating). For docker/no-PID servers render the
  single explanatory line `Plan.Lines()` already returns.
- Test (Update-pumping + injected `previewBoth` fake): enter detail on a
  server whose fake plan has 3 members → `detailView()` output contains all
  three PIDs; docker server → contains `docker stop`.

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

- Add `SortCycle` binding (`s`, help "sort") to the list-mode keymap.
- Model field `sortBy string` (default `"port"`); on `s`:
  cycle port → uptime → name → port, set `m.status = "sort: " + key`,
  `m.rebuild()`.
- In `rebuild`, after `inventory.View(...)` (which sorts by port), apply
  `inventory.Sort(m.rows, m.sortBy)` when `m.sortBy != "port"`.
  Cursor note: keep it simple — the cursor stays at its index (same as a
  refresh today); plan 012's stable sort keeps the order deterministic.
- Header: append the sort key to the meta line when non-default
  (`· sort:uptime`) so state is visible.
- Tests: three fixture servers with distinct uptimes/names; press `s` once →
  rows reorder by uptime (assert first row); press to `name` → reorder;
  wrap back to port.
- README TUI key table: add the `s` row.

**Verify**: `go test ./internal/tui/ -v` → pass.

### Feature 4: Multi-target kill

- `newKillCmd`: `Args: cobra.MinimumNArgs(1)`; `Use: "kill <port|name> [more...]"`.
- `runKill(targets []string, o)`: loop `matchTargets` per target; error
  `no server found matching %q` if ANY target matches nothing (all-or-nothing
  keeps behavior predictable — state this in the Long help); union the
  matches; `fuzzy` = true if any target matched fuzzily; then the existing
  `dedupeUnits` → confirm → loop, unchanged. The `--single` multi-unit
  warning keys off units as today. Confirmation wording: keep
  `About to kill %d target(s) matching %q` but render the target list
  (`strings.Join(targets, ", ")`) — smallest wording change that stays honest.
- Tests (plan-001 harness): `kill 3000 5173` with fixtures on both ports →
  one combined confirmation, both killed; one bogus target among two →
  error before any prompt; duplicate targets (`3000 3000`) → deduped to one
  unit.
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
