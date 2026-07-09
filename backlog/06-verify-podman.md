# 06 — Smoke-test the podman container detection path on a real podman box

**Status:** todo
**Priority:** do this before calling podman "supported" in public README wording
**Owner:** you (needs access to a box where podman is the container runtime)

## Why

The podman fallback in plan 021 wires the docker package to use podman when
`docker` isn't on `PATH`. The podman CLI is widely regarded as CLI-compatible
with Docker for `ps`/`inspect`/`stop`, and the `com.docker.compose.*` labels
that `podman compose` emits are identical to Docker's. The older independent
`podman-compose` tool uses `io.podman.compose.*` labels instead — the code
handles both namespaces (docker-preferring).

But this has never run on a real podman box. The inspect JSON shapes, port
publishing entries in `NetworkSettings.Ports` (especially for rootless
podman), and `podman stop` exit behaviour are what must be verified.

## Steps (run on a podman box without docker installed)

1. Build from source: `go build ./cmd/whence`.
2. Start a compose stack that publishes ports (e.g. a simple `docker-compose.yml`
   with `podman compose up -d`):
   ```sh
   podman compose up -d
   ```
3. Run:
   ```sh
   whence doctor          # check the container runtime is detected as "podman"
   whence list --all      # compose services should appear attributed to their repo
   whence list --json     # confirm the docker/podman-sourced rows are present
   ```
4. Test a kill:
   ```sh
   whence kill <compose-port>
   podman ps              # confirm the container was stopped
   ```
5. If `podman-compose` (the older independent tool) is also available, test that
   path too — create a compose stack with `podman-compose up -d` and verify the
   `io.podman.compose.*` labels are picked up.

## What to check specifically

### `whence doctor`
- Shows `container runtime: podman — N published port(s), M compose-attributed`.
- Does NOT show `not found (compose services won't be detected)`.

### `whence list` / `whence list --all`
- Compose-attributed rows appear with `SRC` = `docker` and the correct project
  name and `DIRECTORY`/`cwd` pointing at the repo.
- `whence list --json` shows `source: "docker"` and `marker: "docker-compose"`
  or `marker: "podman-compose"` as appropriate.

### Port publishing
- Confirm that host ports published by rootless podman containers appear in the
  container inspect's `NetworkSettings.Ports` map with `HostIp`/`HostPort` in
  the same shape as Docker. Rootless podman uses a user-mode networking stack
  (slirp4netns or pasta); verify published ports are visible there.

### `whence kill`
- `whence kill <port>` stops the container (should show `docker stop` method).
- The container is removed from `podman ps`.
- No host processes are accidentally killed.

### `podman-compose` (older tool)
- If available, the `io.podman.compose.*` labels should be read for compose
  attribution, with marker `podman-compose`.
- Docker-preferring: when both namespace labels are present, docker wins.

## If something's broken

- **Ports not visible in inspect**: rootless podman network implementations
  (`slirp4netns`, `pasta`) may not populate `NetworkSettings.Ports` in the
  expected shape. File the gap — it may require a different port discovery
  method for podman.
- **Compose labels differ structurally**: if `podman compose` or
  `podman-compose` use label keys beyond the two namespaces handled, add them
  with `composeLabel` support.
- **`podman stop` exit differs**: the docker package already tolerates partial
  inspect failure; if `podman stop` has divergent exit semantics (e.g. exits
  0 when the container is already gone), verify the kill path still reports
  success correctly.

## Acceptance

- On a podman-only box: `whence list` shows compose-attributed containers,
  `whence kill` stops them, and `whence doctor` reports the runtime.
- The docker-first priority still works (if someone installs `docker` on a
  podman box, docker is used).
