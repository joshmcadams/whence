# Plan 012: Stable sorts and `--watch` input validation

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/inventory internal/cli/list.go`
> On drift, compare excerpts below to live code before proceeding.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (ordering + input validation only)
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

1. **Unstable sorts shuffle tied rows.** `inventory.Sort` uses `sort.Slice`
   (randomized pivot, not stable) for all three keys. `uptime` and `name`
   produce large tie groups (docker-attribution-less rows share
   `Uptime == 0`; unattributed rows share an empty name), so `--watch` frames
   and 5s TUI refreshes can swap tied rows when nothing changed. In the TUI
   the cursor is a row *index*, so a shuffle lands the cursor on a different
   server and a quick `x` opens the confirm for the wrong neighbor (the
   confirm shows the target, so no silent wrong kill — but it's a trap).
2. **`--watch` accepts junk.** `-i 0` (or negative) busy-loops a full
   scan+docker round-trip per iteration; `--watch --json` silently degrades
   to a single JSON dump instead of erroring.

## Current state

- `internal/inventory/inventory.go:117-132`:

  ```go
  func Sort(s []model.Server, by string) {
      switch by {
      case "uptime":
          sort.Slice(s, func(i, j int) bool { return s[i].Uptime > s[j].Uptime })
      case "name":
          sort.Slice(s, func(i, j int) bool { return s[i].DisplayName() < s[j].DisplayName() })
      default:
          sort.Slice(s, func(i, j int) bool {
              if s[i].Port != s[j].Port { return s[i].Port < s[j].Port }
              return s[i].Proto < s[j].Proto
          })
      }
  }
  ```

- `internal/cli/list.go:60` — `if o.watch && !o.asJSON { return watchList(cfg, o) }`
  (the `--watch --json` silent-ignore); `list.go:119` — `time.Sleep(o.interval)`
  with no lower bound. Flag registered at `list.go:44` with default `2*time.Second`.
- `View` calls `Sort(out, "port")` (inventory.go:78); `listOnce` re-sorts by
  the user's key (list.go:87).
- Tests: `internal/inventory/inventory_test.go` (table style),
  `internal/cli/kill_test.go` for CLI-side patterns.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Inventory tests | `go test ./internal/inventory/ -v` | pass |
| Suite | `make lint && make test` | exit 0 |

## Scope

**In scope**:
- `internal/inventory/inventory.go` (Sort), `internal/inventory/inventory_test.go`
- `internal/cli/list.go` (validation in `runListWith`)
- A CLI test file for the validation (`internal/cli/list_test.go`, new)

**Out of scope**:
- Sort key names / flag surface (plan 019 adds a TUI sort key separately).
- The watch rendering loop mechanics.

## Git workflow

- Branch: `advisor/012-stable-sort-watch`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Deterministic total order in `Sort`

Rewrite with `sort.SliceStable` and explicit full tiebreaks so equal-key rows
have ONE order regardless of input permutation:

```go
func Sort(s []model.Server, by string) {
    less := func(i, j int) bool { return defaultLess(s[i], s[j]) }
    switch by {
    case "uptime":
        less = func(i, j int) bool {
            if s[i].Uptime != s[j].Uptime { return s[i].Uptime > s[j].Uptime }
            return defaultLess(s[i], s[j])
        }
    case "name":
        less = func(i, j int) bool {
            a, b := s[i].DisplayName(), s[j].DisplayName()
            if a != b { return a < b }
            return defaultLess(s[i], s[j])
        }
    }
    sort.SliceStable(s, less)
}

// defaultLess is the port-order tiebreak chain: port, proto, address, pid, name.
func defaultLess(a, b model.Server) bool {
    if a.Port != b.Port { return a.Port < b.Port }
    if a.Proto != b.Proto { return a.Proto < b.Proto }
    if a.Address != b.Address { return a.Address < b.Address }
    if a.PID != b.PID { return a.PID < b.PID }
    return a.Name < b.Name
}
```

Tests: for each key, feed the SAME logical set in two different input orders
(including ties: two rows with equal uptime, two with equal DisplayName, two
with equal port+proto but different address/pid) and assert byte-identical
output order both times.

**Verify**: `go test ./internal/inventory/ -v` → pass (existing View/Sort
assertions unchanged — they assert membership/port order, which holds).

### Step 2: Validate watch flags

In `runListWith` (`internal/cli/list.go:54`), before the watch branch:

```go
if o.watch {
    if o.asJSON {
        return errors.New("--watch cannot be combined with --json")
    }
    if o.interval < 500*time.Millisecond {
        return fmt.Errorf("--interval must be at least 500ms (got %s)", o.interval)
    }
}
```

(500ms floor: a full collect includes docker round-trips; anything faster is
a busy-loop against the daemon. The default 2s is untouched.)

Tests in a new `internal/cli/list_test.go` (white-box): call `runListWith`
with `watch: true, asJSON: true` → error mentioning `--json`; with
`watch: true, interval: 0` → error mentioning `500ms`. Both must error
BEFORE any collection happens — assert by pointing config at an empty
`XDG_CONFIG_HOME` temp dir (pattern: `internal/config/config_test.go`) and
relying on the error returning immediately. (If `collect` runs first in your
implementation, reorder — validation precedes work.)

**Verify**: `go test ./internal/cli/ -v` → pass; `make lint && make test` →
exit 0. Manual: `make run ARGS="list --watch --interval 0"` prints the error
and exits non-zero; `make run ARGS="list --watch --json"` likewise.

## Test plan

Steps 1–2 carry the tests; patterns from `internal/inventory/inventory_test.go`
and `internal/config/config_test.go` (env-based config isolation).

## Done criteria

- [ ] `grep -n "sort.Slice(" internal/inventory/inventory.go` → no matches (all SliceStable)
- [ ] Permutation-stability tests pass for all three keys
- [ ] `whence list --watch --json` and `--interval 0` error with clear messages (tests + manual)
- [ ] `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- An existing test pins a tie order that contradicts `defaultLess` — surface
  it rather than silently changing the expectation.
- Validation cannot run before collection without restructuring beyond
  `runListWith` — report.

## Maintenance notes

- Plan 019 adds a TUI sort-cycle key that calls this same `Sort`; the
  determinism here is what keeps the TUI cursor stable across refreshes.
- If a future column becomes sortable, extend `defaultLess`'s chain — never
  reintroduce `sort.Slice`.
