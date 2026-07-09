# Plan 021: Podman fallback for the container detection path

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat bc713ee..HEAD -- internal/docker internal/kill/kill.go internal/cli/doctor.go README.md AGENTS.md backlog`
> Plans 006 and 010 must be DONE (both reshape `internal/docker/docker.go`;
> this plan builds on the seams and guards they added).
>
> **Platform caveat**: no podman box is available for verification.
> Everything here is CLI-compatible by podman's documented design; the plan
> ends by adding a real-machine verification item to `backlog/`, mirroring
> how macOS/Windows verification is tracked.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED (unverifiable-here runtime; mitigated: docker behavior must be provably unchanged, podman is additive best-effort)
- **Depends on**: plans/006, plans/010
- **Category**: direction
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

On Fedora/RHEL-family dev boxes podman is the default container runtime, and
for those users the headline compose-attribution feature silently produces
nothing — `doctor` just says "docker: not found (compose services won't be
detected)". The docker package is nearly runtime-agnostic already: every call
goes through execx with a string binary name, and the attribution key is a
single label read. The genuinely new work is small: binary resolution, a
second compose-label namespace, and doctor wording.

## Current state

- `internal/docker/docker.go:29-32` — `Available()` = `exec.LookPath("docker")`.
- `docker.go:185` (`runningIDs`) and `:194` (`inspectAll`) hardcode
  `"docker"` as argv[0] (after plan 006, `inspectAll` runs through the
  `dockerOutput` seam and tolerates partial failure; after plan 010, inspect
  args carry `--`).
- `docker.go:99-115` (`classifyContainer`) — reads
  `com.docker.compose.project.working_dir` and `com.docker.compose.project`
  (after plan 010, the workdir must pass `isLocalDir`).
- `internal/kill/kill.go:197-212` (`dockerStop`) — hardcodes `"docker"`;
  after plans 001/010 it runs through the `dockerCombinedOutput` seam with
  `--`.
- `internal/cli/doctor.go:77` (shape) — reports docker availability with the
  "compose services won't be detected" hint.
- Label compatibility facts (encode as code comments): `podman compose`
  (the wrapper delegating to a compose provider) emits the same
  `com.docker.compose.*` labels; the older independent `podman-compose` tool
  emits `io.podman.compose.*` equivalents. `podman ps -q --no-trunc` and
  `podman inspect` are CLI-compatible with the docker invocations used here;
  podman container JSON uses the same `Name`/`State.StartedAt`/
  `Config.Labels`/`NetworkSettings.Ports` shapes for these fields.
- `inventory.Collect` treats the whole container path as best-effort
  (errors swallowed) — podman inherits that for free.
- Backlog pattern for off-machine verification: `backlog/04-verify-macos-windows.md`.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Suite | `make lint && make test` | exit 0 |
| Docker/kill tests | `go test ./internal/docker/ ./internal/kill/ -v` | pass |
| Manual (docker box) | `make run ARGS="list --all"` | compose rows unchanged |

## Scope

**In scope**:
- `internal/docker/docker.go` (+ tests): runtime resolution, label namespace
- `internal/kill/kill.go` (+ tests): `dockerStop` uses the resolved runtime
- `internal/cli/doctor.go`: report which runtime was found
- `README.md` (one line in Features/Notes), `AGENTS.md` package-map line for
  `internal/docker`
- New `backlog/06-verify-podman.md`

**Out of scope**:
- The podman REST API / socket — CLI only, like docker.
- Rootless-podman port-semantics special-casing — best-effort; verification
  item covers it.
- Renaming the `docker` package or `model.SourceDocker` — cosmetic churn;
  keep names, document that they cover "the container runtime".

## Git workflow

- Branch: `advisor/021-podman`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Runtime resolution

In `internal/docker/docker.go`:

```go
// Runtime returns the container CLI to use: docker when present, else
// podman (CLI-compatible for the ps/inspect/stop calls whence makes).
// Empty string when neither exists.
func Runtime() string {
    for _, bin := range []string{"docker", "podman"} {
        if _, err := exec.LookPath(bin); err == nil {
            return bin
        }
    }
    return ""
}
```

Resolve ONCE per `Servers()` call (no global caching — matches the
no-persistence philosophy and keeps tests simple): `Available()` becomes
`Runtime() != ""`; thread the binary through `runningIDs(bin)` /
`inspectAll(bin, ids)`. Docker-first order is deliberate (a box with both
almost certainly uses docker's daemon for dev).

### Step 2: Label namespace

In `classifyContainer`, read the compose labels through a helper that tries
both namespaces:

```go
func composeLabel(labels map[string]string, suffix string) string {
    if v := labels["com.docker.compose."+suffix]; v != "" { return v }
    return labels["io.podman.compose."+suffix]
}
```

with suffixes `project.working_dir` and `project`. The plan-010 `isLocalDir`
guard applies identically to both (same call site — verify it still wraps the
result). Marker string: keep `"docker-compose"` for the docker namespace;
use `"podman-compose"` when the value came from the podman namespace (the
helper can return which — smallest shape: a second helper call or a
two-value return).

Tests (struct fixtures, existing style): podman-labeled container with a real
temp workdir → attributed, marker `podman-compose`, confidence 80;
both-namespace container → docker namespace wins; neither → standalone (40).

### Step 3: Kill path uses the same runtime

`dockerStop` currently hardcodes `"docker"`. Give `internal/kill` the
resolved binary without an import cycle: `docker` already imports nothing
from `kill`, and `kill` imports `model` + `execx` only. Cleanest: add the
binary to the call — `dockerStop` calls `docker.Runtime()`? That imports
`docker` from `kill` — check AGENTS.md's dependency table: `kill` is listed
beside `docker`, both above `model`; `kill → docker` creates no cycle
(docker does not import kill) and keeps a single resolution point. Add the
import, replace the literal:

```go
bin := docker.Runtime()
if bin == "" { bin = "docker" } // unreachable in practice; keeps the error message sane
... dockerCombinedOutput(timeout, bin, "stop", "-t", strconv.Itoa(secs), "--", s.Name)
```

Update plan-001's dockerStop arg assertions accordingly (binary name is now
the first arg the fake sees — assert it's `docker` on this box).

### Step 4: doctor + docs

- `doctor.go`: report `container runtime: docker` / `podman` /
  `not found (compose services won't be detected)` — read the current
  docker lines and extend minimally (doctor's GOOS branching license does
  NOT extend to hiding runtime logic here — call `docker.Runtime()`).
- README Features bullet: "Understands Docker Compose (podman works too —
  detected automatically)". AGENTS.md package map, `internal/docker` row:
  note it drives docker-or-podman.
- New `backlog/06-verify-podman.md` modeled on
  `backlog/04-verify-macos-windows.md`: verify on a podman box — `ps`/
  `inspect` shapes, `podman compose` and `podman-compose` label variants,
  rootless port publishing appearing in `NetworkSettings.Ports`, and
  `whence kill` stopping a podman container.

**Verify**: `go test ./internal/docker/ ./internal/kill/ -v` → pass;
`make lint && make test` → exit 0; on this box (docker present):
`make run ARGS="list --all"` — compose rows identical to before, and
`make run ARGS=doctor` names the runtime.

## Test plan

Step 2's fixture tests + step 3's arg assertions; plus one `Runtime()` test
asserting docker-first preference is encoded (can only assert the lookup
order indirectly — a comment-pinning test is overkill; skip and rely on
review).

## Done criteria

- [ ] `grep -n '"docker"' internal/docker/docker.go internal/kill/kill.go` shows no hardcoded argv[0] left outside `Runtime()`/fallback
- [ ] podman-namespace fixtures attribute with marker `podman-compose` (tests)
- [ ] Docker behavior on this box provably unchanged (manual smoke + all pre-existing docker tests untouched)
- [ ] doctor names the runtime; README/AGENTS updated; `backlog/06-verify-podman.md` exists
- [ ] `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- Plans 006/010 not DONE.
- The `kill → docker` import turns out to create a cycle (it shouldn't —
  verify with `go build ./...`); if it does, thread the binary through
  `model.Server` instead? NO — stop and report; that's a design change.
- Podman fields you need differ structurally from the `inspect` struct
  (discoverable only on a podman box) — this is exactly what
  `backlog/06-verify-podman.md` is for; ship the docker-safe code, flag it.

## Maintenance notes

- The verification backlog item is the real gate on calling this "supported";
  README wording should stay soft ("works too") until it runs.
- If a future contributor adds a third runtime (nerdctl is CLI-compatible
  too), `Runtime()`'s list is the only touch point plus a label check.
- Reviewer: confirm the docker-first preference and the kill-path import
  direction.
