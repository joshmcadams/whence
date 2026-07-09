# Plan 010: Harden attribution inputs — bounded file reads, compose workdir validation, `--` separators

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/project internal/docker internal/kill/kill.go`
> On drift, compare excerpts below to live code before proceeding. Coordinate
> with plan 006 (also edits `internal/docker/docker.go`): run sequentially,
> either order.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (guards on inputs that legitimate projects never trip)
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Attribution reads files chosen by untrusted parties. Manifest and README
paths derive from any process's cwd (walked up to `/`) and from the docker
compose `working_dir` label — an arbitrary string any container can carry.
Three exposures:

1. **Unbounded reads**: `os.ReadFile` with no size cap and no file-type
   check. A FIFO named `README.md` blocks `os.ReadFile` forever — the execx
   timeout discipline doesn't cover filesystem reads — so one hostile repo
   hangs every `whence list` and every 5s TUI refresh. A multi-GB manifest
   allocates its full size.
2. **Label trust**: any container with a `com.docker.compose.project.working_dir`
   label gets confidence 80 (above the default show threshold of 50) under an
   arbitrary project name, with the label path never validated against disk —
   name-squatting inside the "what is mine" view, and `project.Description`
   is invoked on the label's path directly (feeding exposure 1 with an
   attacker-chosen path).
3. **Missing `--` separators** in docker invocations — not exploitable today
   (docker names can't start with `-`; `ps -q` emits hex ids) but a refactor
   that feeds a label-sourced string into the same calls would turn a
   leading-dash value into a docker flag. Cheap defense in depth.

## Current state

- `internal/project/project.go` — four raw `os.ReadFile` sites:
  `jsonField` (line 141), `goModule` (line 156), `parseToml` (line 191),
  `readmeSummary` (line 221, up to 5 README candidates). Helpers at
  `project.go:253-255`: `exists`/`isDir` via `os.Stat` (follows symlinks).
- `internal/docker/docker.go:99-115`:

  ```go
  func classifyContainer(c inspect) (*model.Project, int) {
      labels := c.Config.Labels
      workdir := labels["com.docker.compose.project.working_dir"]
      if workdir == "" {
          return nil, confContainer
      }
      name := labels["com.docker.compose.project"]
      if name == "" { name = strings.TrimPrefix(c.Name, "/") }
      return &model.Project{
          Name: name, Root: workdir,
          Description: project.Description(workdir),
          Marker: "docker-compose",
      }, confCompose
  }
  ```

  `confCompose = 80`, `confContainer = 40` (docker.go:18-21); default
  `ConfidenceThreshold` is 50 (`internal/config/config.go:52`).
- `internal/kill/kill.go:208` — `docker stop -t <n> <name>` and
  `internal/docker/docker.go:193` — `docker inspect <ids...>`: no `--`.
- Tests: `internal/project/project_test.go` (temp-dir fixtures),
  `internal/docker/docker_test.go` (hand-built `inspect` structs).

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Project tests | `go test ./internal/project/ -v` | pass |
| Docker tests | `go test ./internal/docker/ -v` | pass |
| Kill tests | `go test ./internal/kill/ -v` | pass |
| Full suite | `make test` | ok |
| Lint | `make lint` | exit 0 |

## Scope

**In scope**:
- `internal/project/project.go`, `internal/project/project_test.go`
- `internal/docker/docker.go` (classifyContainer + inspect `--`),
  `internal/docker/docker_test.go`
- `internal/kill/kill.go` (docker stop `--` only), `internal/kill/kill_test.go`
  (only if plan 001's dockerStop tests exist — update the expected args)

**Out of scope**:
- The confidence *values* (80/40) and threshold semantics.
- `findRoot`'s walk itself (symlinked *directories* on the walk are a
  same-user local concern, accepted).
- Displaying hard identifiers in kill confirmations — already present
  (`describe()` shows pid/container name).

## Git workflow

- Branch: `advisor/010-attribution-hardening`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Bounded, regular-file-only reads in `internal/project`

Add one helper and route all four readers through it:

```go
// maxManifestBytes bounds any single attribution read. Real manifests and
// READMEs are tiny; the cap defends against reading a huge or non-regular
// file (e.g. a FIFO, which would block forever) from an untrusted repo.
const maxManifestBytes = 1 << 20 // 1 MiB

func readSmallFile(path string) ([]byte, bool) {
    fi, err := os.Lstat(path)
    if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxManifestBytes {
        return nil, false
    }
    data, err := os.ReadFile(path)
    if err != nil || len(data) > maxManifestBytes {
        return nil, false
    }
    return data, true
}
```

Replace each `data, err := os.ReadFile(path); if err != nil { ... }` with
`data, ok := readSmallFile(path); if !ok { ... }` in `jsonField`, `goModule`,
`parseToml`, and `readmeSummary`. Note `Lstat` (not `Stat`): a symlink to a
manifest is rejected. That is a small behavior change — a legitimately
symlinked `README.md` now yields no description. Accepted: cheap, safe, and
rare; record it in the commit message.

Tests (temp-dir style, matching existing ones): oversized file (write
`maxManifestBytes+1` bytes) → field empty; FIFO (skip on windows via
`t.Skipf` — `syscall.Mkfifo` is unix-only; put the FIFO test in a
`//go:build !windows` test file or guard with `runtime.GOOS`) → returns
immediately, field empty (the point: no hang — give the test a deadline by
running the read in the main test goroutine; if it hangs, `go test`'s
10-minute default timeout fails it, but add `-timeout 60s` to the verify
command to keep the loop tight); symlink to a real manifest → empty; normal
manifest → unchanged behavior (existing tests already cover).

**Verify**: `go test ./internal/project/ -timeout 60s -v` → pass.

### Step 2: Validate the compose workdir before trusting it

In `classifyContainer`, require the labeled path to exist as a directory
before granting compose attribution:

```go
workdir := labels["com.docker.compose.project.working_dir"]
if workdir == "" || !isLocalDir(workdir) {
    return nil, confContainer
}
```

with `func isLocalDir(p string) bool` doing `filepath.IsAbs(p)` +
`os.Stat` + `IsDir()` (Stat, not Lstat — a compose project checked out via a
symlinked path is legitimate; the guard's job is "this label points at a real
directory on THIS machine", which also naturally fails for containers built
elsewhere). Deliberately NOT requiring `IsUnderDevRoot`: compose projects
outside dev roots are legitimate; existence is the spoof-resistance floor,
and the confidence system remains the "mine" arbiter.

Tests (hand-built `inspect` structs): label pointing at a real `t.TempDir()`
→ compose attribution with confidence 80 (existing behavior); label
`/nonexistent/xyz` → `nil` project, confidence 40; relative-path label
`../../etc` → confidence 40; empty label → unchanged.

**Verify**: `go test ./internal/docker/ -v` → pass.

### Step 3: `--` separators

- `internal/kill/kill.go:208`:
  `execx.CombinedOutput(timeout, "docker", "stop", "-t", strconv.Itoa(secs), "--", s.Name)`
- `internal/docker/docker.go:193`:
  `args := append([]string{"inspect", "--"}, ids...)`

If plan 001's `dockerStop` tests landed, update their expected-args
assertions to include `--`.

**Verify**: `go test ./internal/kill/ ./internal/docker/` → pass;
`make lint && make test` → exit 0. If a docker daemon is available, one
manual smoke: `make run ARGS="list --all"` still shows compose rows.

## Test plan

Steps 1–2 carry the tests; patterns from `internal/project/project_test.go`
(temp dirs) and `internal/docker/docker_test.go` (struct fixtures).

## Done criteria

- [ ] `grep -n "os.ReadFile" internal/project/project.go` shows exactly one hit (inside `readSmallFile`)
- [ ] Oversized/FIFO/symlink tests pass with `-timeout 60s`
- [ ] Nonexistent-workdir container drops to confidence 40 with nil project (test proves it)
- [ ] Both docker invocations carry `--` before positional untrusted args
- [ ] `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- `docker stop --` or `docker inspect --` is rejected by the installed docker
  CLI (verify once with a throwaway container if a daemon is available; if no
  daemon, note it and proceed — `--` is POSIX-standard for both commands).
- The Lstat symlink rejection breaks an existing test that deliberately uses
  a symlinked manifest — surface it; the accepted-change rationale above may
  need operator sign-off.
- Plan 006 landed with conflicting edits to `classifyContainer` — rebase
  mechanically or report.

## Maintenance notes

- Plan 021 (podman) adds a second label namespace (`io.podman.compose.*`);
  the `isLocalDir` guard must apply to that path too — 021's plan says so.
- If large legitimate READMEs (>1 MiB) ever surface, only the description
  goes missing; raise `maxManifestBytes` rather than removing the check.
- Reviewer: confirm no call path still reads attribution files without the
  helper (search for `os.ReadFile` in `internal/project`).
