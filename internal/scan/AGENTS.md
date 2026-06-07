# AGENTS.md — `internal/scan`

This package turns listening sockets into `model.Server`s. The non-obvious part —
and the project's central cross-platform risk — is resolving a process's current
working directory, which has a different mechanism on every OS.

## The cwd contract

Every OS file implements exactly this:

```go
func processCwd(pid int32) (string, error)
```

build-tagged so exactly one compiles per target. Do not add a `runtime.GOOS`
switch in `scan.go`; add/adjust the per-OS file.

| File | Build tag | Mechanism | Notes |
|------|-----------|-----------|-------|
| `cwd_linux.go` | `linux` | `readlink /proc/<pid>/cwd` | Also covers WSL. Other users' procs → permission error. |
| `cwd_darwin.go` | `darwin` | parse `lsof -a -p <pid> -d cwd -Fn` | Makes `lsof` a **runtime dependency** on macOS (`doctor` reports it). gopsutil has no Darwin `Cwd()`. |
| `cwd_windows.go` | `windows` | gopsutil reads the PEB via `NtQueryInformationProcess` | **Least-exercised path** — not run on the Linux/WSL dev box. Verify on a real Windows host. |

## Rules

- A cwd failure is **never fatal**: return `("", err)` and let `enrich` record it
  as a `Server.Notes` entry. The row must still appear. Same for exe/cmdline/ppid.
- Prefer the **executable basename** for `Server.Name`, not gopsutil `Name()`,
  which reads `/proc/pid/comm` and can be a thread name (e.g. `MainThread` for a
  node/python server). See `enrich` in `scan.go`.
- Dedup key is `port/proto/pid`. A process bound to both IPv4 and IPv6 therefore
  yields two rows (`tcp` + `tcp6`); if you change that, decide deliberately
  whether per-stack visibility is wanted.

If you add a new per-OS capability, update `doctor.go` so it reports the
dependency, and add a note to the root `AGENTS.md` caveats list.
