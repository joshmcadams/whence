# imp-01 — Time-bound every external command

**Status:** todo
**Priority:** high — robustness; affects the default command for everyone
**Category:** robustness / correctness
**Effort:** ~45 min

## Problem

Every shell-out runs with no timeout and no cancellation:

- `internal/docker/docker.go:160` — `docker ps -q --no-trunc`
- `internal/docker/docker.go:169` — `docker inspect <ids…>`
- `internal/scan/cwd_darwin.go:21` — `lsof -a -p <pid> -d cwd -Fn`
- `internal/kill/kill.go:155` — `docker stop -t <n> <name>`
- `internal/kill/signal_windows.go:14,18` — `taskkill`

`grep -rn "context\." --include="*.go"` returns nothing: the project never uses
`exec.CommandContext`.

If the Docker daemon is wedged (starting up, socket permission stall, a hung
`containerd`), `docker ps` blocks indefinitely. Because `inventory.Collect`
calls `docker.Servers()` on the **default** `whence` / `whence list` path, the
whole CLI hangs with no output and no way to tell why. The same is true of the
TUI's first load and its 5-second auto-refresh tick.

This directly violates a stated invariant in `AGENTS.md`:

> **Docker is best-effort.** A missing/broken Docker must never break
> `whence list`.

A daemon that *hangs* (rather than being absent) breaks it today.

## Why it matters

`whence list` is the headline command and the no-arg default. A multi-second
(or unbounded) hang on a flaky-Docker machine is the worst possible first
impression, and it's invisible to the user — no spinner, no error, just a frozen
terminal.

## Suggested approach

Add a small helper and route all shell-outs through it:

```go
// internal/<pkg>/... or a tiny shared internal/execx package
func runContext(timeout time.Duration, name string, args ...string) ([]byte, error) {
    ctx, cancel := context.WithTimeout(context.Background(), timeout)
    defer cancel()
    return exec.CommandContext(ctx, name, args...).Output()
}
```

- **Docker discovery** (`docker ps`, `docker inspect`): wrap with a short budget
  (e.g. 3s). On `ctx.Err() == context.DeadlineExceeded`, return `(nil, err)` so
  `inventory.Collect` swallows it exactly as it already swallows other Docker
  errors — the listing proceeds with native processes only. `doctor` should
  report the timeout distinctly ("docker: found, but timed out").
- **macOS `lsof`**: a per-PID timeout (e.g. 2s) so one stuck process can't stall
  the scan; on timeout, record it as a `Server.Notes` entry (the row still
  appears), consistent with the existing cwd-failure handling.
- **`docker stop` / `taskkill`**: bound by the kill timeout already in `Opts`
  (plus a small margin), so a kill can't hang forever either.

Keep the per-OS build-tag discipline: the `lsof` change stays in
`cwd_darwin.go`, the `taskkill` change in `signal_windows.go`.

## Tests / verification

- Unit: inject a fake command runner (a function field) into the docker package
  and assert that a deadline-exceeded error yields `nil` servers, not a hang.
- Manual: `sudo systemctl stop docker` (or stop Docker Desktop mid-start) and
  confirm `whence list` returns native results within the budget instead of
  hanging.

## Notes / trade-offs

- A timeout that's too tight will drop Docker results on a slow-but-healthy
  daemon (first query after boot). 3s is a reasonable default; consider making
  it a config knob (`docker_timeout_seconds`) if it proves fiddly.
- This pairs naturally with **imp-08** (overlap the Docker query with the native
  scan so the budget is hidden behind work you're doing anyway).
