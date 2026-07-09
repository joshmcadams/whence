# Plan 018: macOS scan robustness — socket-enumeration timeout, batched lsof, testable parser

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat bc713ee..HEAD -- internal/scan internal/cli/doctor.go AGENTS.md`
> Plan 005 must be DONE — this plan plugs into the `rowsFromConns`/enrich
> shape it created.
>
> **Platform caveat**: this box is Linux/WSL. Darwin code here is verified by
> cross-compilation and by unit-testing the extracted pure logic; real-Mac
> verification is tracked in `backlog/04-verify-macos-windows.md` and MUST be
> flagged in your final report as still required.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED (darwin path cannot be executed here; mitigations: pure-logic extraction + compile gates + the backlog verification item)
- **Depends on**: plans/005-scan-row-correctness.md
- **Category**: bug + perf
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Three macOS problems, all in the scan path, all invisible from this dev box:

1. **The socket enumeration itself shells out to lsof with no timeout.**
   `gnet.Connections("inet")` on darwin resolves to gopsutil's
   `net_unix.go`, which runs `lsof -i tcp -i udp` via
   `CallLsofWithContext(context.Background(), ...)` — no deadline (verified
   in the module cache at v4.26.5). A wedged lsof hangs `whence list`, the
   TUI, and `kill` forever — precisely the failure the repo's execx invariant
   promises can't happen; it slips through because the shell-out hides inside
   a dependency.
2. **Missing lsof kills the whole scan, and the docs say otherwise.**
   An lsof-not-found error from `Connections` becomes the fatal
   `enumerate sockets:` error. `doctor` and both AGENTS files claim lsof is
   only needed for *cwd resolution* — decision-doc drift; on a Mac without
   lsof, whence is fully nonfunctional, not "rows without cwd".
3. **Per-process lsof is an N+1.** `processCwd` spawns one
   `lsof -a -p <pid> -d cwd -Fn` per process, sequentially, each costing
   100–300ms+ — 1–3s added to every list/refresh for 10 listeners. lsof
   accepts a comma-separated pid list and `-Fpn` output tags each `n` line
   with its `p` pid, so one call resolves every cwd.

## Current state

- `internal/scan/scan.go:20-23`:

  ```go
  func Processes() ([]model.Server, error) {
      conns, err := gnet.Connections("inet")
      if err != nil {
          return nil, fmt.Errorf("enumerate sockets: %w", err)
      }
  ```

  gopsutil exposes `gnet.ConnectionsWithContext(ctx, kind)`; the darwin
  implementation honors ctx for the lsof subprocess (`common_unix.go`
  passes ctx to `exec.CommandContext`). On Linux, ConnectionsWithContext
  reads /proc and the context is effectively free — safe to use
  unconditionally in shared code.

- `internal/scan/cwd_darwin.go` (whole file, 37 lines): `processCwd(pid)`
  shells `lsof -a -p <pid> -d cwd -Fn` through execx (2s timeout), scans for
  the line starting with `n`.
- After plan 005, `scan.Processes` = `Connections` → `rowsFromConns(conns,
  now, enrich)` → collapse → sort; `enrich` calls `processCwd(pid)` per
  attributed row (`scan.go:129` at `caec51a`).
- `internal/cli/doctor.go:54-60` — reports lsof as the *cwd* dependency on
  darwin ("macOS cwd needs `lsof`" framing, matching AGENTS.md:100 and
  `internal/scan/AGENTS.md`).
- Build-tag layout: `cwd_linux.go` / `cwd_darwin.go` / `cwd_windows.go` each
  define `processCwd(pid int32) (string, error)`.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Scan tests | `go test ./internal/scan/ -v` | pass |
| Darwin compile | `GOOS=darwin GOARCH=arm64 go build ./... && GOOS=darwin GOARCH=amd64 go build ./...` | exit 0 |
| Other compiles | `GOOS=windows GOARCH=amd64 go build ./... && go build ./...` | exit 0 |
| Suite | `make lint && make test` | exit 0 |

## Scope

**In scope**:
- `internal/scan/scan.go` (ConnectionsWithContext + timeout; batch-cwd hook)
- `internal/scan/cwd_darwin.go` (batched resolver)
- `internal/scan/cwd_linux.go`, `internal/scan/cwd_windows.go` (only if the
  hook signature requires a per-OS no-op — see Step 3 design)
- New `internal/scan/lsof.go` (untagged pure parser) + tests in
  `internal/scan/lsof_test.go`
- `internal/cli/doctor.go` (darwin lsof wording)
- `AGENTS.md` + `internal/scan/AGENTS.md` (lsof dependency truth)

**Out of scope**:
- gopsutil version changes.
- Windows cwd path.
- Any change to per-row error semantics (failures stay Notes).

## Git workflow

- Branch: `advisor/018-macos-scan`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Bound the socket enumeration

In `scan.go`:

```go
// scanTimeout bounds the socket enumeration. On darwin gopsutil shells out
// to lsof for this (not just for cwd), and a wedged lsof must time out
// rather than hang every command — the same rule execx enforces for our own
// shell-outs. On linux/windows the enumeration is syscalls and never
// approaches this.
const scanTimeout = 10 * time.Second

ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
defer cancel()
conns, err := gnet.ConnectionsWithContext(ctx, "inet")
```

Keep the existing fatal-error wrap. Add to the wrap a hint when
`ctx.Err() != nil`: `fmt.Errorf("enumerate sockets: timed out after %s: %w", scanTimeout, err)`
(mirrors execx's timed-out phrasing).

**Verify**: `go build ./... && go test ./internal/scan/` → pass (linux path
unchanged; the context is plumbing only).

### Step 2: Extract the lsof field-output parser (pure, untagged)

New `internal/scan/lsof.go` — NO build tag:

```go
// parseLsofCwds parses `lsof -a -p <pids> -d cwd -Fpn` field output:
// 'p'-prefixed lines carry a pid, 'n'-prefixed lines carry the cwd for the
// most recent pid. Returns pid → cwd for every pair found.
func parseLsofCwds(out []byte) map[int32]string
```

Parsing rules (lsof -F emits one field per line): on `p<digits>` set the
current pid (ignore parse failures); on `n<path>` assign to the current pid
if one is set; ignore `f`/other field lines. Tests in `lsof_test.go`
(runs on every OS — that's the point):

1. Single pid: `"p123\nn/Users/x/dev/app\n"` → `{123: "/Users/x/dev/app"}`.
2. Batch: `"p1\nn/a\np2\nn/b\n"` → both.
3. Missing cwd for one pid: `"p1\np2\nn/b\n"` → only 2.
4. Empty / garbage input → empty map, no panic.
5. Path containing spaces → preserved.

**Verify**: `go test ./internal/scan/ -run TestParseLsofCwds -v` → pass on
this (Linux) box.

### Step 3: Batched cwd resolution on darwin

Design: add an optional batch hook next to the per-pid one, defaulting to a
loop, overridden on darwin.

- In shared `scan.go`: enrichment currently calls `processCwd` per row.
  Restructure `Processes` to collect the attributed pids first, call
  `processCwds(pids)` ONCE, and pass the resulting map into enrich (a new
  parameter or a closure — keep `rowsFromConns`'s signature stable per plan
  005's maintenance note; the map lookup happens inside the `enrichFn`
  closure `Processes` builds).
- `cwd_linux.go` / `cwd_windows.go`: add

  ```go
  // processCwds resolves cwds pid-by-pid; cheap syscalls on this OS.
  func processCwds(pids []int32) map[int32]string {
      out := make(map[int32]string, len(pids))
      for _, pid := range pids {
          if cwd, err := processCwd(pid); err == nil && cwd != "" {
              out[pid] = cwd
          }
      }
      return out
  }
  ```

  BUT per-row error notes must survive: today a cwd failure appends a
  `"cwd: ..."` note. Change the map to `map[int32]cwdResult` with
  `{path string; err error}` so enrich can keep writing notes — or return
  two maps. Pick the smallest shape that preserves note behavior exactly;
  the existing note-producing tests (if any) plus a new one are the gate.
- `cwd_darwin.go`: `processCwds` issues ONE
  `execx.Output(lsofBatchTimeout, "lsof", "-a", "-p", strings.Join(pidStrs, ","), "-d", "cwd", "-Fpn")`
  and feeds `parseLsofCwds`. `lsofBatchTimeout = 5 * time.Second` (batch does
  more work than the old per-pid 2s). Pids missing from the result map get a
  `cwd: not reported by lsof` note via the same result shape. Keep the
  existing single-pid `processCwd` only if something still calls it;
  otherwise delete it (the parser is now the tested core).
  Note: lsof exits non-zero if ANY listed pid has no cwd fd — so on error,
  still parse stdout (same partial-tolerance pattern as plan 006's
  inspectAll) and only treat empty output as a real failure, recorded as a
  note on every attributed row, never a scan abort.

**Verify**: `go test ./internal/scan/ -v` → pass;
`GOOS=darwin GOARCH=arm64 go build ./... && GOOS=darwin GOARCH=amd64 go build ./...`
→ exit 0; `GOOS=windows GOARCH=amd64 go build ./...` → exit 0.

### Step 4: True up doctor and the AGENTS notes

- `internal/cli/doctor.go` darwin section: report lsof as required for the
  scan itself ("socket enumeration and cwd resolution both require lsof on
  macOS; without it whence cannot list servers at all").
- `AGENTS.md` "Known caveats": change "macOS cwd needs `lsof`" to reflect
  that the whole scan needs it; `internal/scan/AGENTS.md`: same correction
  plus one line describing the batch resolver.

**Verify**: `make lint && make test` → exit 0.

## Test plan

Step 2's parser table (cross-platform) + a shared-code test that
`processCwds`' results flow into rows/notes correctly (fake the hook on
linux by testing the linux `processCwds` against real `/proc/self` — its
own pid is a valid target). The darwin subprocess plumbing is compile-gated
only — say so in the report.

## Done criteria

- [ ] `grep -n "gnet.Connections(" internal/scan/scan.go` → no matches (context variant used)
- [ ] `internal/scan/lsof.go` has no build tag; its tests pass on Linux
- [ ] Darwin `processCwds` makes exactly one lsof invocation (code inspection; note in report)
- [ ] Per-row cwd failures still surface as Notes (test proves at least one note path)
- [ ] doctor + both AGENTS files state the true lsof dependency
- [ ] All four cross-compiles and `make lint && make test` exit 0
- [ ] Final report flags real-Mac verification as outstanding (backlog 04)
- [ ] `plans/README.md` status row updated

## STOP conditions

- Plan 005 not DONE.
- `gnet.ConnectionsWithContext` does not exist or ignores ctx on darwin in
  the pinned gopsutil (check `~/go/pkg/mod/github.com/shirou/gopsutil/v4@v4.26.5/net/net_unix.go`
  and `internal/common/common_unix.go`) — report what you find.
- Preserving exact note semantics forces `rowsFromConns` signature churn that
  conflicts with plan 005's landed shape — report the conflict.
- Anything requires actually running lsof here — it can't be; the parser
  tests are the executable surface.

## Maintenance notes

- Real-Mac validation checklist for backlog 04: `whence list` shows cwd-based
  attribution; `whence doctor` reports lsof; killing a Vite dev server works;
  a deliberately slow lsof (e.g. suspended) times the scan out at ~10s with
  the timed-out message instead of hanging.
- If gopsutil ever implements native darwin cwd (`proc_pidinfo`), the batch
  resolver and lsof dependency collapse — the parser file just gets deleted.
- The `scanTimeout` and `lsofBatchTimeout` constants are the knobs if Macs
  with hundreds of listeners report timeouts.
