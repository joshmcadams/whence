# Plan 015: Cleanup sweep — dead code, config.Path build tags, error idioms

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/project internal/tui/theme.go internal/cli/collect.go internal/cli/list.go internal/cli/kill.go internal/config internal/kill/kill.go internal/scan`
> Plan 013 must be DONE — it converts `internal/cli/collect.go` into a test
> seam that this sweep must NOT delete.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW (deletions of verified-unused code + mechanical moves)
- **Depends on**: plans/013-remaining-test-seams.md
- **Category**: tech-debt
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Small drift traps, each verified at planning time:

1. **Dead exported API**: `project.Detect` (package-level) has zero
   non-test callers and duplicates `Cache.Detect`'s construction line-for-line
   — a change landing in one and not the other is the trap. `tui.ThemeNames`
   ("for help/config docs") is referenced nowhere, tests included.
2. **Convention breach**: `config.Path()` branches on `runtime.GOOS` inside a
   shared file; AGENTS.md names `doctor.go` as the ONE sanctioned exception.
   Either the code or the doc is wrong — fix the code (the convention is the
   repo's own).
3. **Error idioms**: `dockerStop` wraps with `%v` (breaks `errors.Is/As`
   chains) where the rest of the repo uses `%w`; the "no accessible pid"
   advice string is written twice in `kill` (and a third variant lives in
   `scan`), already drifted ("try elevated privileges" vs "rerun with
   elevated privileges").
4. **Misplaced helper**: generic `filter()` sits in `list.go` but its only
   caller is `matchTargets` in `kill.go`.

## Current state

- `internal/project/project.go:60-74` — package-level `Detect`; verified:
  `grep -rn "project\.Detect" --include='*.go'` finds only
  `project_test.go` usages (classify uses `Cache.Detect`; docker uses
  `project.Description`).
- `internal/tui/theme.go:53-60` — `ThemeNames()`; zero references anywhere.
- `internal/config/config.go:57-69`:

  ```go
  func Path() string {
      if runtime.GOOS == "windows" {
          if base := os.Getenv("AppData"); base != "" {
              return filepath.Join(base, "whence", "config.toml")
          }
      }
      if base := os.Getenv("XDG_CONFIG_HOME"); base != "" { ... }
      home, _ := os.UserHomeDir()
      return filepath.Join(home, ".config", "whence", "config.toml")
  }
  ```

  Build-tag pattern to copy: `internal/scan/cwd_linux.go` /
  `cwd_windows.go` / `cwd_darwin.go` (one small function per file).
- `internal/kill/kill.go:105` and `:149` — the duplicated
  `no accessible pid (owned by another user; try elevated privileges)`;
  `internal/scan/scan.go:51` — the variant
  `no pid (owned by another user; rerun with elevated privileges)`.
- `internal/kill/kill.go:208-209` — `fmt.Errorf("%v: %s", err, out)`.
- `internal/scan/cwd_linux.go:18` — replaces a permission error with a bare
  `fmt.Errorf("permission denied")`, discarding the original (verify the
  exact line in the live file; wrap instead).
- `internal/cli/list.go:123-131` — `filter()`; only caller
  `internal/cli/kill.go:107-113`.
- `internal/cli/collect.go` — after plan 013 it is `var collect = func(...)`;
  KEEP IT.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Suite | `make lint && make test` | exit 0 |
| Windows compile | `GOOS=windows GOARCH=amd64 go build ./...` | exit 0 |
| Darwin compile | `GOOS=darwin GOARCH=arm64 go build ./...` | exit 0 |
| Dead-code recheck | `grep -rn "ThemeNames\|project\.Detect" --include='*.go' .` | test files only, before deleting |

## Scope

**In scope**:
- `internal/project/project.go`, `internal/project/project_test.go`
- `internal/tui/theme.go`
- `internal/config/config.go` + new `internal/config/path_windows.go`,
  `internal/config/path_unix.go`
- `internal/kill/kill.go` (+ `kill_test.go` if messages are asserted)
- `internal/scan/scan.go` / `internal/scan/cwd_linux.go` (message/wrap only)
- `internal/cli/list.go`, `internal/cli/kill.go` (move `filter`)
- `AGENTS.md` — no change needed once the code conforms (the "one exception"
  sentence becomes true); touch it only if it enumerates `config.Path`.

**Out of scope**:
- `internal/cli/collect.go` — plan 013's seam; do not delete.
- `internal/cli/doctor.go` — the sanctioned GOOS exception stays.
- Any behavior change; this is deletion, relocation, and message/wrap hygiene.

## Git workflow

- Branch: `advisor/015-cleanup-sweep`
- One commit per numbered step keeps review trivial.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Delete dead code

- Re-run the dead-code recheck grep (see Commands) to confirm nothing new
  references them, then: delete `ThemeNames` from `theme.go`; delete the
  package-level `Detect` from `project.go` and retarget its tests at
  `NewCache().Detect(...)` (assertions unchanged — same inputs/outputs).

**Verify**: `go build ./... && go test ./internal/project/ ./internal/tui/` → pass.

### Step 2: Build-tag `config.Path`

- Split per OS, matching the `cwd_*.go` pattern (one small function per
  build-tagged file, each owning the full decision):
  - `internal/config/path_windows.go` (`//go:build windows`): `configBase()`
    returns `filepath.Join(os.Getenv("AppData"), "whence")` when `AppData`
    is set, else falls through to the shared XDG/home logic (keep that
    fallback logic as an unexported helper in `config.go` so both files can
    call it).
  - `internal/config/path_unix.go` (`//go:build !windows`): `configBase()`
    returns the XDG/home dir via the same shared helper.
  - `Path()` in `config.go` becomes
    `filepath.Join(configBase(), "config.toml")` — no GOOS branch, no
    `runtime` import.
- Preserve exact behavior, including the Windows fallback-to-XDG-when-
  AppData-unset quirk (yes, it's odd; preserving it keeps this plan
  zero-behavior-change — note it in the commit message).
- Existing config tests use `XDG_CONFIG_HOME` isolation and run on linux —
  they must pass untouched.

**Verify**: `go test ./internal/config/ -v` → pass;
`GOOS=windows GOARCH=amd64 go build ./...` → exit 0;
`grep -n "runtime.GOOS" internal/config/` → empty.

### Step 3: Error hygiene in kill/scan

- `dockerStop`: `fmt.Errorf("%w: %s", err, out)`.
- Hoist the no-pid message: `var errNoPID = errors.New("no accessible pid (owned by another user; try elevated privileges)")`
  in `kill.go`; use it at both sites (`Plan.Lines` prints `errNoPID.Error()`,
  `Server` returns it).
- `scan.go:51`'s note stays a *note* (different surface, "no pid" prefix is
  scan-specific) but align the advice tail to one phrasing — pick
  "rerun with elevated privileges" for both files or unify on the kill one;
  choose ONE and apply to all three sites.
- `cwd_linux.go`: wrap instead of replace —
  `fmt.Errorf("permission denied: %w", err)` (or whatever preserves the
  current user-visible prefix; read the live line first).

**Verify**: `go test ./internal/kill/ ./internal/scan/` → pass (update any
test that pins the old drifted variant — list such updates in the report).

### Step 4: Move `filter`

Cut `filter()` from `list.go`, paste into `kill.go` above `matchTargets`.

**Verify**: `make lint && make test` → exit 0; both cross-compiles → exit 0.

## Test plan

No new tests beyond retargeting `Detect`'s. The gates are the greps,
cross-compiles, and the full suite.

## Done criteria

- [ ] `grep -rn "func Detect" internal/project/` → only `Cache.Detect` remains
- [ ] `grep -rn "ThemeNames" .` → no matches
- [ ] `grep -rn "runtime.GOOS" internal/ --include='*.go' | grep -v doctor.go | grep -v _test.go` → empty
- [ ] `grep -c "no accessible pid" internal/kill/kill.go` → message defined once (var), used twice
- [ ] `internal/cli/collect.go` still exists as the seam var
- [ ] `make lint && make test`, windows+darwin cross-compiles all exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- Plan 013 not DONE (collect-seam ordering).
- The dead-code recheck grep finds a NEW caller of `Detect`/`ThemeNames`
  (another plan wired it up — e.g. a future config/help change): skip that
  deletion, note it.
- Preserving `config.Path`'s exact Windows fallback behavior proves
  impossible under build tags (it won't — but if the helper split forces a
  behavior choice, report it).

## Maintenance notes

- `configBase` is now the pattern for any future per-OS path (cache dirs,
  state files).
- If `whence config` ever grows theme listing/help output, REINTRODUCE
  `ThemeNames` deliberately at that point (plan 020 does not need it).
- Reviewer: confirm the unified privilege-advice phrasing reads correctly in
  both the scan note (list row) and the kill error contexts.
