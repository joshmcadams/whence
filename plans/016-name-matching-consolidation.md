# Plan 016: Consolidate the three name-matching implementations

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/model internal/inventory internal/cli/kill.go`
> On drift, compare excerpts below to live code before proceeding.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED (matching is user-facing behavior with deliberate per-surface differences; the point is to preserve them explicitly)
- **Depends on**: none
- **Category**: tech-debt
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

"Does this server match the string `foo`?" is answered three different ways:

- **Query filter** (`inventory.matchesQuery`): lowercase *substring* over
  `DisplayName()`, `Description()`, and the port digits.
- **Ignore list** (`inventory.isIgnored`): lowercase *exact* over `Name` and
  `DisplayName()` — but NOT `Project.Name` explicitly (DisplayName covers it
  only when a project exists).
- **Kill target** (`cli.nameMatches`): exact-then-substring over
  `DisplayName()`, `Name`, AND `Project.Name`.

Each surface consults a different field set, so users get three subtly
different behaviors for one mental operation, and every new name source
(e.g. a compose service name) must be threaded into three functions. The
per-surface *policies* (exact-first for kill, exact-only for ignore,
substring for query) are deliberate and must survive; the *field sets* and
normalization should be one definition.

## Current state

- `internal/inventory/inventory.go:85-105` — `isIgnored` (ports loop, then
  lowercase-exact over `s.Name` / `s.DisplayName()` vs trimmed entries).
- `internal/inventory/inventory.go:107-115` — `matchesQuery`
  (substring over DisplayName, Description, `strconv.Itoa(s.Port)`).
- `internal/cli/kill.go:105-137` — `matchTargets` (port fast-path; exact
  pass, then substring pass with `fuzzy=true`) and `nameMatches` (names
  slice: `DisplayName()`, `Name`, `+ Project.Name` when non-nil).
- `internal/model/model.go` — `DisplayName()` prefers `Project.Name`, falls
  back to `Name` (read the live implementation before coding).
- Documented kill semantics (README + `cli/kill.go` Long help): "A name
  prefers an exact (case-insensitive) match and only falls back to substring
  when there is none." README on ignore lists: "`ignore_names` matches a
  process/container name or a project name (case-insensitive)." ← note the
  README ALREADY claims project names match the ignore list; the code only
  matches them via DisplayName. Unifying the field set makes the README true.
- Tests: `internal/cli/kill_test.go` (matchTargets cases — keep green
  unchanged), `internal/inventory/inventory_test.go` (isIgnored/View cases).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Model tests | `go test ./internal/model/ -v` | pass |
| Inventory tests | `go test ./internal/inventory/ -v` | pass |
| CLI tests | `go test ./internal/cli/ -v` | pass |
| Suite | `make lint && make test` | exit 0 |

## Scope

**In scope**:
- `internal/model/model.go`, `internal/model/model_test.go` (add `Names()`)
- `internal/inventory/inventory.go`, `internal/inventory/inventory_test.go`
- `internal/cli/kill.go` (rewrite `nameMatches` on the shared accessor)

**Out of scope**:
- The per-surface policies: exact-first kill, exact-only ignore,
  substring query — all preserved bit-for-bit.
- `matchesQuery`'s Description and port-digit matching (query-only features;
  they stay local to the query path).
- README changes (its ignore-list claim becomes true; nothing to edit).

## Git workflow

- Branch: `advisor/016-name-matching`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: `model.Server.Names()`

```go
// Names returns every name this server can be addressed by, most specific
// first: project name, display name, raw process/container name. Empty
// entries are omitted; no deduplication is promised.
func (s Server) Names() []string
```

(Project name when `s.Project != nil`, then `DisplayName()`, then `Name`.)
Table-test: project+name server → 3 entries; nameless project-less → just
`Name`; fully empty → empty slice.

### Step 2: Matching helpers in `inventory`

`model` must stay logic-light (dependency sink) — the predicates live in
`inventory`, which both consumers can reach (`cli` already imports it; this
plan makes `internal/cli/kill.go` import it for the predicate if it doesn't
already — check the import list; `runKill` file currently imports
`config/kill/model` only, and `matchTargets` is called from kill.go — adding
the `inventory` import there is fine and does not create a cycle:
`cli → inventory` is the sanctioned direction).

```go
// NameEquals reports whether want (already lowercased) exactly equals any of
// the server's Names(), case-insensitively.
func NameEquals(s model.Server, want string) bool

// NameContains is the substring form of NameEquals.
func NameContains(s model.Server, want string) bool
```

### Step 3: Migrate the three call sites

- `isIgnored`: keep the ports loop and the per-entry trim/lower; replace the
  two-field comparison with `NameEquals(s, n)`. Behavior delta: ignore
  entries now also match a bare `Project.Name` even when `DisplayName()`
  differs — today they can't differ (DisplayName IS Project.Name when a
  project exists), so this is a no-op in practice; state that in the commit.
- `matchesQuery`: replace the DisplayName clause with `NameContains(s, q)`;
  KEEP the Description and port clauses as-is. Behavior delta: the query now
  also matches raw `Name` when a project renames the display — deliberate
  small improvement; note it.
- `cli.nameMatches`: body becomes
  `if exact { return inventory.NameEquals(s, want) } ; return inventory.NameContains(s, want)`.
  `matchTargets`' exact-then-substring structure is untouched.

**Verify** each migration: `go test ./internal/inventory/ ./internal/cli/ -v`
→ ALL pre-existing cases pass unchanged (this is the real gate — the
matchTargets table in `kill_test.go` encodes the exact-first policy).

### Step 4: New tests

- `inventory_test.go`: `NameEquals`/`NameContains` tables (project vs raw
  name, case-insensitivity, empty want never matches).
- One regression case per migrated surface exercising the shared field set
  (e.g. ignore entry equal to a project name; query matching a raw container
  name).

**Verify**: `make lint && make test` → exit 0.

## Test plan

Steps 1 and 4; pattern: existing tables in `inventory_test.go` and
`kill_test.go`.

## Done criteria

- [ ] `model.Server.Names()` exists with tests
- [ ] `grep -n "strings.ToLower(s.Name)" internal/inventory/inventory.go` → no matches (field logic centralized)
- [ ] All pre-existing matchTargets/isIgnored/View tests pass UNCHANGED
- [ ] The two stated behavior deltas are covered by new tests and named in the report
- [ ] `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- Any pre-existing matching test fails — the consolidation changed a policy;
  report the case rather than editing the test.
- The `cli → inventory` import turns out to already exist in a form that
  creates awkward layering for the predicates — if you're tempted to put the
  predicates in `model` instead, stop and report (model is the dependency
  sink; helper predicates over its own fields are arguably fine there, but
  that's an operator call).

## Maintenance notes

- New name sources (compose service name, k8s labels, plan 021's podman
  names) get added to `Names()` ONCE and every surface follows.
- The per-surface policy wrappers are now thin enough that a future
  `--exact` kill flag or globbing lands in one place each.
