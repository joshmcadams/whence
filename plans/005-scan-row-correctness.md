# Plan 005: Scan row correctness — address-aware dedup, `*` exposure, testable extraction

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/scan internal/model internal/docker/docker.go`
> On drift, compare excerpts below to live code before proceeding.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW-MED (touches the hot path of every scan; behavior-preserving extraction plus two targeted fixes)
- **Depends on**: none
- **Category**: bug + tests
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Three related defects in how sockets become rows:

1. **Dedup eats distinct listeners.** The dedup key is `(port, proto, pid)`,
   omitting the bind address. Two *different* unattributed processes (both
   `Pid == 0` in an unprivileged scan) listening on the same port/proto but
   different addresses collapse to one row under `--all`. Stock Linux desktop
   case: systemd-resolved on `127.0.0.53:53` and libvirt's dnsmasq on
   `192.168.122.1:53` — one silently disappears. This contradicts the
   documented promise that "genuinely distinct IP bindings are left untouched".
2. **macOS wildcard binds are never flagged.** `Exposure()` classifies only
   `""`, `"0.0.0.0"`, `"::"` as all-interfaces. On darwin, gopsutil parses
   `lsof -n` output where a wildcard bind renders as `*:PORT`, so
   `Laddr.IP == "*"` — the `[!]` reachable-off-box warning can never fire on
   macOS, the platform the tool most targets.
3. **"All-interfaces" knowledge lives in three files** (`model.Exposure`,
   `docker.isAllInterfacesIP`, the collapse constants in `scan`); adding the
   `"*"` case in one place and not the others is exactly the drift this
   invites. Centralize while fixing.

The scan loop is also untested (the collapse helper has tests; the
LISTEN-filter/dedup/no-PID-note loop does not), so fix #1 needs an extraction
to be testable at all.

## Current state

- `internal/scan/scan.go:20-64` — `Processes()`:

  ```go
  conns, err := gnet.Connections("inet")
  ...
  for _, c := range conns {
      if strings.ToUpper(c.Status) != "LISTEN" { continue }
      proto := protoOf(c.Family)
      key := fmt.Sprintf("%d/%s/%d", c.Laddr.Port, proto, c.Pid)   // ← no address
      if seen[key] { continue }
      seen[key] = true
      s := model.Server{ Port: int(c.Laddr.Port), Proto: proto,
          Address: c.Laddr.IP, Source: model.SourceProcess, PID: int(c.Pid) }
      if c.Pid > 0 { enrich(&s, c.Pid, now) } else {
          s.Notes = append(s.Notes, "no pid (owned by another user; rerun with elevated privileges)")
      }
      servers = append(servers, s)
  }
  servers = collapseIPv4IPv6(servers)
  sort.Slice(servers, ...)
  ```

- `internal/scan/scan.go:73-100` — `collapseIPv4IPv6` merges `(port, pid)`
  pairs with equal exposure class; the normalization switch hard-codes
  `"0.0.0.0"` / `"127.0.0.1"`. Its behavior is pinned by `scan_test.go` and is
  a documented invariant (AGENTS.md "Dual-stack servers … collapse to one
  row") — do not change its semantics, only extend the address vocabulary.

- `internal/model/model.go:71-80`:

  ```go
  func (s Server) Exposure() string {
      switch s.Address {
      case "127.0.0.1", "::1", "localhost":
          return "local"
      case "", "0.0.0.0", "::":
          return "all"
      default:
          return s.Address
      }
  }
  ```

- `internal/docker/docker.go:180-182`:

  ```go
  func isAllInterfacesIP(ip string) bool {
      return ip == "" || ip == "0.0.0.0" || ip == "::"
  }
  ```

- `model` imports nothing internal (AGENTS.md invariant) — helpers can be
  ADDED to model; model must not import scan/docker.
- Tests: `internal/scan/scan_test.go` (collapse + protoOf cases, white-box),
  `internal/model/model_test.go` (display helper cases).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Scan tests | `go test ./internal/scan/ -v` | pass |
| Model tests | `go test ./internal/model/ -v` | pass |
| Docker tests | `go test ./internal/docker/ -v` | pass |
| Full suite | `make test` | ok |
| Lint | `make lint` | exit 0 |
| Cross-compile | `GOOS=darwin GOARCH=arm64 go build ./... && GOOS=windows GOARCH=amd64 go build ./...` | exit 0 |

## Scope

**In scope**:
- `internal/scan/scan.go`, `internal/scan/scan_test.go`
- `internal/model/model.go`, `internal/model/model_test.go`
- `internal/docker/docker.go` (delegate `isAllInterfacesIP` to model; no other change)

**Out of scope**:
- `collapseIPv4IPv6` merge *semantics* (which rows merge) — only the address
  vocabulary via the shared helper.
- `enrich()` internals and the per-OS `processCwd` files.
- `internal/inventory` — plan 006's territory.
- Any UDP or non-inet handling.

## Git workflow

- Branch: `advisor/005-scan-row-correctness`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Centralize address classification in `model`

In `internal/model/model.go` add:

```go
// IsAllInterfaces reports whether a bind address means "every interface".
// "*" is how lsof (and therefore gopsutil on darwin) renders a wildcard bind.
func IsAllInterfaces(addr string) bool {
    switch addr {
    case "", "0.0.0.0", "::", "*":
        return true
    }
    return false
}

// IsLoopback reports whether a bind address is loopback-only.
func IsLoopback(addr string) bool {
    switch addr {
    case "127.0.0.1", "::1", "localhost":
        return true
    }
    return false
}
```

Rewrite `Exposure()` in terms of them (same return values: `"local"`,
`"all"`, else the literal address). This adds `"*"` → `"all"`, fixing the
macOS `[!]` warning.

Model tests: extend the existing `Exposure` cases (or add `TestExposure`)
covering `"*"` → `"all"`, plus one case per existing class to pin behavior.

**Verify**: `go test ./internal/model/ -v` → pass.

### Step 2: Delegate `docker.isAllInterfacesIP`

```go
func isAllInterfacesIP(ip string) bool { return model.IsAllInterfaces(ip) }
```

(`internal/docker` already imports `internal/model`.) No behavior change for
docker's current inputs; `"*"` never appears in docker inspect output, and if
it ever did, "all" is the right answer.

**Verify**: `go test ./internal/docker/` → pass.

### Step 3: Extract the conns→rows transformation

In `internal/scan/scan.go`, extract the loop body of `Processes()` into a
pure function so it can be table-tested:

```go
// rowsFromConns converts raw connection stats into unenriched server rows:
// LISTEN filtering, per-(port,proto,address,pid) dedup, and the no-PID note.
// enrichFn is called for rows with a PID; injected for testability.
func rowsFromConns(conns []gnet.ConnectionStat, now time.Time,
    enrichFn func(*model.Server, int32, time.Time)) []model.Server
```

`Processes()` becomes: `Connections("inet")` → `rowsFromConns(conns, now, enrich)`
→ `collapseIPv4IPv6` → sort → return. Behavior identical except Step 4's key.

**Verify**: `go build ./... && go test ./internal/scan/` → existing tests pass.

### Step 4: Include the bind address in the dedup key

Inside `rowsFromConns`:

```go
key := fmt.Sprintf("%d/%s/%s/%d", c.Laddr.Port, proto, c.Laddr.IP, c.Pid)
```

The dual-stack pair (`0.0.0.0` + `::`, same pid) now survives this dedup as
two rows — which is exactly what `collapseIPv4IPv6` downstream is for; it
merges them back to one. The rows that newly survive are the genuinely
distinct bindings (different addresses), including unattributed (`Pid == 0`)
ones, which the collapse deliberately leaves alone.

Also update `collapseIPv4IPv6`'s exposure-class handling to use the model
helpers where it hard-codes membership checks, keeping its output constants
(`"0.0.0.0"`, `"127.0.0.1"`) as-is. If a merged pair's addresses were
`"*"`-classified, the "all" branch already normalizes to `"0.0.0.0"` — fine.

**Verify**: `go test ./internal/scan/ -v` → all existing collapse tests pass
UNCHANGED (if one fails, read it — it may pin the buggy dedup; see STOP).

### Step 5: Tests for `rowsFromConns`

Table tests with hand-built `[]gnet.ConnectionStat` (construct
`gnet.ConnectionStat{Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: ..., Port: ...}, Pid: ...}`;
a no-op `enrichFn` that records calls):

1. Non-LISTEN entries are dropped (e.g. `ESTABLISHED`).
2. Exact duplicates (same port/proto/address/pid) dedup to one row.
3. **The fix**: two rows, same port+proto, `Pid: 0`, addresses
   `127.0.0.53` and `192.168.122.1` → BOTH survive (this fails against the
   old key).
4. `Pid: 0` row gets the `no pid` note and `enrichFn` is not called for it.
5. `Pid > 0` row: `enrichFn` called once with that pid.
6. Dual-stack same-pid pair (`0.0.0.0` fam 2 + `::` fam 10) → two rows out of
   `rowsFromConns` (collapse handles the merge; assert the collapse end-state
   separately by piping through `collapseIPv4IPv6` → one row, proto `tcp`,
   address `0.0.0.0`).

Plus one end-state test for `"*"`: a row with Address `"*"` has
`Exposure() == "all"` (belongs in model tests, step 1, but assert here too if
a scan-level fixture makes it natural — don't duplicate excessively).

**Verify**: `go test ./internal/scan/ -v` → all pass;
`make lint && make test` → exit 0; both cross-compiles exit 0.

## Test plan

Steps 1 and 5 carry the tests. Patterns: existing table style in
`internal/scan/scan_test.go` and `internal/model/model_test.go`.

## Done criteria

- [ ] `grep -n '"%d/%s/%d"' internal/scan/scan.go` returns no matches (old key gone)
- [ ] `grep -rn '== "0.0.0.0"' internal/docker/docker.go` returns no matches (delegated)
- [ ] `Exposure()` returns `"all"` for `"*"` (test proves it)
- [ ] The two-distinct-listeners-port-53 test passes
- [ ] All pre-existing scan/model/docker tests pass unchanged
- [ ] `make lint`, `make test`, darwin+windows cross-compiles exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- A pre-existing `scan_test.go` case fails after Step 4 and the failure is NOT
  obviously the old-key behavior being pinned — report with the test name and
  diff before editing any existing test.
- `gnet.ConnectionStat` cannot be constructed in tests as described (field
  shapes differ in gopsutil v4.26.5) — check the struct in
  `~/go/pkg/mod/github.com/shirou/gopsutil/v4@v4.26.5/net/net.go`, then adapt
  or report.
- You find yourself changing which rows `collapseIPv4IPv6` merges — invariant
  territory, stop.

## Maintenance notes

- Plan 018 (macOS batched lsof) plugs into the `enrichFn`/`Processes` shape
  this plan creates — keep `rowsFromConns`'s signature stable.
- Any future exposure vocabulary (e.g. `"[::]"` from some tool) goes into
  `model.IsAllInterfaces` ONLY — that's the point of Step 1.
- Reviewer: the dual-stack collapse invariant (AGENTS.md) must demonstrably
  still hold — the step-5 case 6 test is the proof to look at.
