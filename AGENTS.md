# AGENTS.md — working in `whence`

Guidance for AI agents (and humans who like terse docs) working in this repo.
For the product-level "what and why," read `README.md`; for the original
architecture rationale, read `DESIGN.md`. This file is the build/convention/
invariant cheat-sheet.

## What this is

`whence` is a cross-platform Go CLI + TUI that finds the dev servers and
databases the current user is running on local ports, maps each listening port
back to the repo it was launched from, and kills them by port or by project.
Single static binary per OS/arch. No daemon, no persistence — every command is
an on-demand snapshot.

## Commands

```sh
make build        # -> bin/whence (injects version via -ldflags)
make test         # go test ./...
make lint         # gofmt -l + go vet (+ golangci-lint if installed)
make fmt          # gofmt -w .
make run ARGS=... # e.g. make run ARGS="list --all"  /  make run ARGS=tui

go test ./internal/kill/   # a single package
go build ./...             # plain compile, no version stamp
```

Always run `make lint` and `make test` before declaring a change done. CI
(`.github/workflows/ci.yml`) runs golangci-lint (incl. gofmt via formatters),
`go test`, govulncheck, a cross-compile matrix (linux/darwin/windows ×
amd64/arm64), and `goreleaser check` — keep all of those green. Toolchain:
Go 1.25+ (see `go.mod`); dev boxes on newer Go are fine.

## Architecture — the data flow

```
scan.Processes() ─┐                            (native host processes)
                  ├─► inventory.Collect() ─► classify.Process() ─► []model.Server
docker.Servers() ─┘        (merge,                (score + attach
   (containers)         dedup by port)             project via project.Detect)

[]model.Server ─► inventory.View(all, port, query) ─► output.Table / output.JSON
                          (shared filter)         └─► tui (Bubble Tea) renders rows
                                                  └─► kill.Server() terminates
```

`inventory` is the seam: both the CLI (`internal/cli`) and the TUI
(`internal/tui`) call `inventory.Collect` + `inventory.View` so list output and
the interactive table can never diverge. Don't add a second collection path —
extend `inventory`.

## Package map

| Package | Responsibility |
|---------|----------------|
| `cmd/whence` | `main`; just calls `cli.Execute()`. |
| `internal/cli` | cobra command tree (`list`, `kill`, `tui`, `config`, `doctor`) + the default `whence` = `list`. |
| `internal/model` | shared `Server` / `Project` types and their display helpers. The dependency sink — it imports nothing internal. |
| `internal/execx` | run external commands with a hard timeout. A leaf util (imports nothing internal); **every shell-out goes through it**, never `os/exec` directly. |
| `internal/config` | load/save TOML config, dev-root matching (`IsUnderDevRoot`), XDG/`%AppData%` path resolution. |
| `internal/scan` | enumerate listening TCP sockets + owning process; **per-OS cwd resolution** (build-tagged). See `internal/scan/AGENTS.md`. |
| `internal/project` | walk cwd → repo root; extract name + description from manifests / README. |
| `internal/classify` | confidence score ("is this mine?") + dev-command hints. |
| `internal/docker` | parallel detection path: `docker ps`/`inspect` → compose attribution, k8s filtering. |
| `internal/inventory` | merge native + docker, the shared `View` filter, and `Sort`. |
| `internal/kill` | tree-kill of native processes + `docker stop`; **per-OS signals**. See `internal/kill/AGENTS.md`. |
| `internal/output` | table / JSON rendering, `HumanUptime`, `Truncate`. |
| `internal/tui` | Bubble Tea model, keybindings, themes. |

Dependency direction is one-way: `model` ← everything; `cli`/`tui` sit on top of
`inventory` which sits on `scan`/`docker`/`classify`/`project`. Keep it acyclic —
nothing below `inventory` should import `cli`, `tui`, or `inventory`.

## Conventions

- **Errors that concern one row are notes, not failures.** A process whose cwd
  or cmdline can't be read still appears, with the reason appended to
  `Server.Notes`. Only a failure to enumerate sockets at all aborts a scan. Keep
  this resilience — don't turn a per-process error into a hard return.
- **Docker is best-effort.** `inventory.Collect` deliberately ignores the error
  from `docker.Servers()`; a missing/broken Docker must never break `whence list`.
- **External commands run with a timeout.** Every shell-out (`docker`, `lsof`,
  `taskkill`) goes through `internal/execx`, never `os/exec` directly, so a
  wedged dependency times out instead of hanging `whence`. A real non-zero exit
  passes through unchanged; only a deadline becomes a `timed out` error. The one
  allowed direct `os/exec` use is `exec.LookPath` for availability checks (no
  child process is spawned). `execx.Interactive` is the sanctioned form for
  user-launched interactive children (no timeout, inherited stdio) — everything
  else keeps the timeout rule.
- **Cross-platform code is build-tagged, one file per OS** (`cwd_linux.go`,
  `cwd_darwin.go`, `cwd_windows.go`, `signal_unix.go`, `signal_windows.go`). Add
  platform behavior by adding/editing these, never with `runtime.GOOS`
  branches inside shared files (`doctor.go` is the one intentional exception,
  for diagnostics).
- **Tests are white-box** (`package foo`, same dir) and drive real logic: the
  TUI tests pump `Update()`; kill tests exercise `climb`/`subtree` on synthetic
  process tables. New behavior in `kill`, `classify`, `project`, `docker`,
  `inventory`, or `tui` should come with a test in the same style.
- Module path is `github.com/joshmcadams/whence`. If the GitHub owner changes,
  update `go.mod`, the `-X ...cli.version` ldflags path (Makefile +
  `.goreleaser.yaml`), and the goreleaser tap/bucket owners together.

## Invariants you must not break

- **`kill` never climbs through a shell or init.** `climb` stops at any
  non-launcher parent and at pid ≤ 1, so an interactive session is never
  killed. The `launchers` allow-list is curated (and deliberately excludes bare
  `node`). Read `internal/kill/AGENTS.md` before touching it.
- **Attribution reads the process *tree*, not just the leaf.** `npm run dev` →
  `node` means the project cwd may live on the wrapper or the leaf; killing
  targets the climbed tree so the wrapper isn't orphaned and the port is freed.
- **`model` imports nothing internal.** It's the shared vocabulary; pushing
  logic into it that reaches back up creates cycles.

## Known caveats / sharp edges

- **Unprivileged scans see only your own processes.** Root-owned listeners
  (system Postgres, docker-proxy, k3s) show no PID and appear unattributed under
  `--all`. This is expected, surfaced in `doctor`, and noted on the row.
- **`kill <port>` can terminate more than the named port.** Because the kill
  climbs to a launcher and takes the whole subtree, a shared launcher (e.g. one
  `make dev` recipe that starts several services directly) means killing one
  port stops its siblings too. By design — and the confirmation prompt now
  enumerates the full tree (climbed root + descendants, with a process count) so
  you see every process before agreeing. The preview and the kill share
  `kill.planTree`, so they can't disagree (see `internal/kill/AGENTS.md`, which
  also covers the npm/yarn/pnpm case — the climb usually stops at the shell
  those wrappers interpose, not at the wrapper itself).
- **Dual-stack servers (bound to both IPv4 and IPv6) collapse to one row.**
  `scan.collapseIPv4IPv6` merges `(port, pid)` pairs where both stacks share
  the same exposure class — `0.0.0.0`+`::` → `tcp`/`0.0.0.0`; `127.0.0.1`+`::1`
  → `tcp`/`127.0.0.1`. Genuinely distinct IP bindings and unattributed rows
  (PID ≤ 0) are left untouched. This is intentional — do not revert to the
  old per-proto dedup key without updating `scan_test.go`.
- **macOS requires `lsof` for both socket enumeration and cwd resolution**
  (gopsutil shells out to lsof inside `gnet.Connections`); Windows cwd reads
  the PEB via gopsutil and is the
  least-exercised path. `doctor` reports both.

## Off-machine work

Distribution/release steps that can't be done from this dev box live in
`backlog/` (add remote, publish tap/bucket, cut `v0.1.0`, verify macOS/Windows
cwd paths). Start with `backlog/README.md`.
