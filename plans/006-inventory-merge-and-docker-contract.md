# Plan 006: Inventory merge correctness + docker JSON contract tests

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/inventory internal/docker`
> On drift, compare excerpts below to live code before proceeding.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED (changes which native rows are suppressed; guarded by table tests)
- **Depends on**: none (coordinate with plan 010, which also edits `internal/docker/docker.go` — run sequentially, either order)
- **Category**: bug + tests
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Three gaps around the docker/native merge — the single seam both CLI and TUI
depend on:

1. **Merge suppresses by bare port number.** `Collect` drops every native row
   whose port equals any container-published port, ignoring address. A native
   listener coexisting with a container on the same port number via disjoint
   bind addresses is invisible in every view, and `whence kill <port>` then
   targets only the container while reporting success.
2. **`docker inspect` partial failure discards the whole batch.** One
   container exiting between `docker ps -q` and the `inspect` call makes the
   inspect exit non-zero, and `inspectAll` returns nothing — every docker row
   vanishes for that cycle. The TUI rescans every 5s and `--watch` every 2s,
   so this window recurs constantly on busy machines; docker being
   best-effort makes it silent.
3. **Neither behavior is tested, and the docker JSON tags never execute.**
   `Collect` hardwires `scan.Processes()`/`docker.Servers()` so its merge rule
   can only run against the live machine; docker tests build `inspect` structs
   directly, so a wrong JSON tag (`HostIp` vs `HostIP`) would pass the whole
   suite while producing empty addresses in production.

## Current state

- `internal/inventory/inventory.go:20-52` — `Collect`:

  ```go
  func Collect(cfg config.Config) ([]model.Server, error) {
      type dockerResult struct{ servers []model.Server }
      dockerCh := make(chan dockerResult, 1)
      go func() {
          s, _ := docker.Servers() // best-effort; error deliberately ignored
          dockerCh <- dockerResult{s}
      }()
      procs, err := scan.Processes()
      if err != nil { return nil, err }
      classify.Process(procs, cfg)
      dockers := (<-dockerCh).servers

      dockerPorts := make(map[int]bool, len(dockers))
      for _, d := range dockers { dockerPorts[d.Port] = true }

      merged := make([]model.Server, 0, len(procs)+len(dockers))
      merged = append(merged, dockers...)
      for _, p := range procs {
          if dockerPorts[p.Port] { continue }        // ← suppression by bare port
          merged = append(merged, p)
      }
      return merged, nil
  }
  ```

- `internal/docker/docker.go:192-203` — `inspectAll`:

  ```go
  func inspectAll(ids []string) ([]inspect, error) {
      args := append([]string{"inspect"}, ids...)
      out, err := execx.Output(dockerTimeout, "docker", args...)
      if err != nil { return nil, err }              // ← stdout discarded on exit 1
      var containers []inspect
      if err := json.Unmarshal(out, &containers); err != nil { return nil, err }
      return containers, nil
  }
  ```

  `docker inspect a b` with one unknown id exits 1 but still prints the JSON
  array of found containers on stdout (errors go to stderr; `execx.Output`
  captures stdout only and passes non-zero exits through as errors).

- `internal/docker/docker.go:34-49` — the `inspect` struct with JSON tags
  (`"Name"`, `"State"→"StartedAt"`, `"Config"→"Image"/"Labels"`,
  `"NetworkSettings"→"Ports"` with `"HostIp"`/`"HostPort"`). No test
  unmarshals real JSON into it today.

- `internal/execx/execx.go` — `Output(timeout, name, args...)` returns
  stdout bytes and an error; on non-zero exit both may be non-nil-ish
  (out has whatever was written before exit). Read it before Step 2 to
  confirm; the timeout case returns a `timed out` error.

- The suppression exists to hide the native `docker-proxy` listener that
  mirrors each published container port (README "How it works" + DESIGN.md).
  Typical proxy row: root-owned → `PID <= 0`, unattributed; when the scan IS
  privileged, `Name` is `docker-proxy` (Linux) or the Docker Desktop backend
  process.

- Docker error-swallowing in `Collect` is a decided tradeoff — keep it.
- Tests: `internal/inventory/inventory_test.go` (View/isIgnored cases) and
  `internal/docker/docker_test.go` (hostPorts/classifyContainer/isKubernetes
  via hand-built structs) — white-box, table-driven.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Inventory tests | `go test ./internal/inventory/ -v` | pass |
| Docker tests | `go test ./internal/docker/ -v` | pass |
| Full suite | `make test` | ok |
| Lint | `make lint` | exit 0 |

## Scope

**In scope**:
- `internal/inventory/inventory.go`, `internal/inventory/inventory_test.go`
- `internal/docker/docker.go` (only `inspectAll`), `internal/docker/docker_test.go`
- `internal/docker/testdata/inspect.json` (new fixture)

**Out of scope**:
- The concurrency shape of `Collect` (goroutine + channel) — unchanged.
- `classify.Process`, `scan.Processes` — untouched.
- The error-swallow of `docker.Servers()` in `Collect` — decided tradeoff.
- `runningIDs`, `classifyContainer` internals (plan 010 handles workdir
  validation; plan 021 handles the runtime binary).

## Git workflow

- Branch: `advisor/006-inventory-merge`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Extract the merge into a pure, tested function

In `inventory.go`:

```go
// merge combines docker and native rows. A native row is suppressed only when
// it is docker's own listener for a published port: same port AND either
// (a) the row names the docker proxy machinery, (b) the row is unattributed
// (PID ≤ 0 — the root-owned proxy in an unprivileged scan), or (c) the
// container binding covers the native row's address (same exposure class,
// or the container is bound to all interfaces). A native listener on a
// genuinely different interface than the container survives.
func merge(dockers, procs []model.Server) []model.Server
```

Implementation guide:
- Build `map[int][]model.Server` of docker rows by port.
- For each native row with a matching port, suppress iff ANY docker row on
  that port satisfies:
  `isDockerProxyName(p) || p.PID <= 0 || d.Exposure() == "all" || d.Exposure() == p.Exposure()`.
- `isDockerProxyName(s model.Server) bool`: name or exe basename equal to
  `docker-proxy`, or name prefixed `com.docker.` (Docker Desktop), or equal
  to `rootlesskit` (rootless Docker). Case-sensitive is fine — these are
  fixed binary names.
- Order preserved: dockers first, then surviving procs (same as today).

`Collect` calls `merge(dockers, procs)`. Today's observable behavior is
preserved for the common cases (the proxy is unattributed or proxy-named, so
it's still suppressed); what changes is the rare distinct-interface case, and
that's the fix.

**Verify**: `go build ./... && go test ./internal/inventory/` → existing pass.

### Step 2: Table tests for `merge`

In `inventory_test.go`:

1. Classic proxy case: docker row port 5433 exposure all; native row port
   5433, `PID: 0` → suppressed (one row out, the docker one).
2. Privileged proxy case: native row port 5433, `PID: 900`,
   `Name: "docker-proxy"` → suppressed.
3. **The fix**: docker row port 8080, Address `127.0.0.1`; native row port
   8080, `PID: 4242`, `Name: "python3"`, Address `192.168.1.5` → BOTH
   survive (fails against the old rule).
4. Container bound to all interfaces suppresses a same-port attributed native
   row with exposure "all" (can't genuinely coexist; treat as proxy/mirror).
5. Disjoint ports: nothing suppressed; order = dockers then procs.
6. Empty docker set: procs pass through untouched.

**Verify**: `go test ./internal/inventory/ -v` → all pass.

### Step 3: Tolerate partial `docker inspect` failure

In `inspectAll`, parse stdout even when the command errored:

```go
out, err := execx.Output(dockerTimeout, "docker", args...)
var containers []inspect
if jsonErr := json.Unmarshal(out, &containers); jsonErr == nil && len(containers) > 0 {
    return containers, nil // partial success: some ids resolved before exit 1
}
if err != nil { return nil, err }
return containers, nil
```

(A real timeout or daemon failure yields empty/invalid stdout → the old error
path is preserved. An all-ids-unknown run prints `[]` → empty slice, falls to
the `err != nil` return — also preserved.)

To test without a docker daemon, seam the exec call as a package var,
matching the pattern plan 001 establishes in `internal/kill`:

```go
var dockerOutput = execx.Output
```

Tests: (a) valid JSON + non-nil error → containers returned, nil error;
(b) empty output + error → error propagates; (c) valid JSON + nil error →
normal path.

**Verify**: `go test ./internal/docker/ -run TestInspectAll -v` → pass.

### Step 4: Fixture test for the JSON contract

Create `internal/docker/testdata/inspect.json`: a realistic
`docker inspect` array with THREE entries (write it by hand from the struct's
expectations — or, if a docker daemon is available locally, capture
`docker inspect <some-container>` and redact):

1. A compose container: `"Name": "/jfdid-db-1"`, labels
   `com.docker.compose.project` + `com.docker.compose.project.working_dir`,
   `State.StartedAt` in RFC3339Nano, `NetworkSettings.Ports` with a
   `"5432/tcp"` entry (`HostIp: "0.0.0.0"`, `HostPort: "5433"`) AND a
   `"5432/udp"` entry (must be skipped) and one binding with empty
   `HostPort` (must be skipped).
2. A k8s container: name `/k8s_something`, one `io.kubernetes.pod.name`
   label (must be filtered by `isKubernetes`).
3. A standalone container: no compose labels, ipv6 binding `HostIp: "::"`
   normalizing to `0.0.0.0`.

Test: `os.ReadFile` the fixture, `json.Unmarshal` into `[]inspect` (the same
type `inspectAll` uses), then assert through the real helpers:
`isKubernetes(c2) == true`; `hostPorts(c1)` yields exactly one entry
`{5433, "tcp", "0.0.0.0"}`; `parseTime(c1.State.StartedAt)` is non-zero;
`parseTime("garbage")` is zero; `classifyContainer(c1)` returns a project
named from the compose label with `Marker == "docker-compose"` (temp-dir
trick not needed — `project.Description` on a nonexistent workdir returns ""
today; just don't assert the description).

**Verify**: `go test ./internal/docker/ -v` → all pass;
`make lint && make test` → exit 0.

## Test plan

Steps 2–4 are the test plan. Patterns: table style from
`internal/inventory/inventory_test.go` and `internal/docker/docker_test.go`;
fixture-under-`testdata/` is the standard Go convention (first use in this
repo — fine).

## Done criteria

- [ ] `merge` exists, is called by `Collect`, and the distinct-interface test (Step 2 case 3) passes
- [ ] `inspectAll` returns parsed containers when stdout holds valid JSON despite a non-zero exit (test proves it)
- [ ] `internal/docker/testdata/inspect.json` exists; the fixture test exercises `json.Unmarshal` into `inspect` and passes
- [ ] All pre-existing inventory/docker tests pass unchanged
- [ ] `make lint` and `make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- The suppression rule as specified would demonstrably re-show docker-proxy
  duplicate rows on THIS machine (`make run ARGS="list --all"` with a compose
  stack up shows a proxy row next to its container row) — report the actual
  row data instead of tweaking the rule ad hoc.
- `execx.Output` turns out to discard stdout on non-zero exit (read
  `internal/execx/execx.go` first) — Step 3's premise fails; report.
- Plan 010 landed first and its `docker.go` changes conflict beyond trivial
  rebase — report rather than merging judgment calls silently.

## Maintenance notes

- If docker rootless setups surface new proxy binary names, extend
  `isDockerProxyName` — one function, tested.
- Plan 021 (podman) threads a runtime binary through `runningIDs`/
  `inspectAll`; the `dockerOutput` seam added here is what it will reuse.
- Reviewer: case 4 of the merge tests encodes a judgment call (all-interfaces
  container suppresses same-port native "all" row); confirm you agree with
  the reasoning in the code comment.
