# `ports` — dev server / port tracker

A cross-platform CLI + TUI that finds the dev servers and databases **you** are
running, maps each listening port back to the repo it was launched from, and
lets you kill them by port or by project.

- **Language:** Go (single static binary per OS/arch; Charm Bubble Tea TUI; `gopsutil` for ports/PIDs/uptime).
- **Kill scope:** whole process tree — graceful `SIGTERM`, then `SIGKILL` after a timeout.
- **Detection:** a port is "yours" if the owning process's working dir is under a configured dev root **or** sits in/under a repo (`.git`, `package.json`, `go.mod`, `Makefile`, …). Combined into a confidence score.
- **Persistence:** none. On-demand snapshot; uptime derived from process start time.
- **Docker:** in scope. A parallel detection path attributes compose-run services (incl. databases) back to their repo via the compose `working_dir` label.

> **Validated on the dev machine (WSL, Go 1.26.1).** The full path works end-to-end:
> a Vite server on `:5173` → `pid 351638` → cwd `~/development/personal/jfdid/web` → start time → repo. A compose
> Postgres on `:5433` (`jfdid-db-1`) is owned by `docker-proxy` (no cwd link) but its `com.docker.compose.project.working_dir`
> label resolves to `~/development/personal/jfdid` — exactly why the Docker path is required. A native Postgres on `:5432`
> showed **no PID** to unprivileged `ss` (root-owned) — confirming the privilege caveat. A k3s cluster is also present;
> its `k8s_*` containers carry `io.kubernetes.*` labels and are filtered out as infra by default.

---

## 1. How it works (the pipeline)

```
listening sockets ──▶ owning PID ──▶ process info ──▶ repo root ──▶ "is this mine?" ──▶ project metadata
   (port, proto)        (pid)        (cwd, cmd,        (walk up      (confidence)        (name, description)
                                      start, ppid)      from cwd)
```

1. **Enumerate listening sockets.** `gopsutil/net.Connections("inet")`, keep `LISTEN`, dedupe by `(proto, port)`. IPv4 + IPv6.
2. **Resolve the owning process.** cmdline, exe, parent PID, **start time** (→ uptime), and the **current working directory**.
3. **Find the repo root.** Walk up from the process cwd looking for markers: `.git`, `go.mod`, `package.json`, `Makefile`, `pyproject.toml`, `Cargo.toml`, `composer.json`, etc.
4. **Classify "mine."** Score: cwd/root under a configured dev root → high; repo marker present → medium; dev-server-looking command (`vite`, `next`, `npm run dev`, `nodemon`, `rails s`, `uvicorn`, `air`, `dlv`, a `postgres`/`redis`/`mongod` under a repo, …) → boost. Show if score ≥ threshold; `--all` shows everything.
5. **Project metadata.** Name from `package.json:name` / `go.mod` module / dir name. Description from `package.json:description` / `pyproject` / `Cargo.toml` / first paragraph or first heading of `README.md`.

### The hard part: process → working directory (per-OS)

This is the central cross-platform risk and is built **first**.

| OS | Method |
|----|--------|
| Linux / WSL | `readlink /proc/<pid>/cwd` — trivial |
| Windows | `NtQueryInformationProcess` → PEB → `ProcessParameters.CurrentDirectory` (via `golang.org/x/sys/windows`) |
| macOS | **`gopsutil` does not implement `Cwd()` on Darwin.** Fall back to parsing `lsof -a -p <pid> -d cwd -Fn`; optional cgo `proc_pidinfo(PROC_PIDVNODEPATHINFO)` later. Note `lsof` may also be load-bearing for gopsutil's *socket enumeration* on Darwin — the Phase-1 spike must confirm whether macOS needs `lsof` and/or root for the whole path, and `doctor` should report it as a hard dep if so. |

### The other subtlety: the listener is usually a child

`npm run dev` → `node` → the process actually holding the socket. So:
- **Attribution** walks the process tree, not just the leaf — the project cwd may live on the leaf or a parent.
- **Killing** targets the tree (see below), so the wrapper doesn't get orphaned and the port is actually freed.

### Docker / compose detection (parallel path)

Container-run services hold their socket via `docker-proxy` / the Docker backend, whose cwd is unrelated to your
repo — so the cwd heuristic alone misses them. Instead:

1. Query the Docker API (or `docker ps`/`inspect`) for running containers and their published port mappings.
2. Attribute each back to a repo via labels: `com.docker.compose.project.working_dir` (→ repo root) and
   `com.docker.compose.project` (→ name). Description still comes from that repo's README.
3. **Filter out non-dev containers:** anything carrying `io.kubernetes.*` labels (k3s/k8s infra) is excluded by
   default; only compose projects (and optionally standalone containers under a dev root) count as "yours."
4. Merge with the native-process results, deduping by published port.

Killing a compose service means `docker compose stop` / `docker stop <container>`, **not** a host signal — tracked
as a distinct kill backend.

### Environment boundary

The tool reports **the machine it runs on**. Inside WSL it sees the Linux netns; on the Windows host it sees Windows. That's intentional — two separate environments, same binary.

### Privileges

Your own dev servers (same user) need no elevation. Reading another user's process cwd needs root on some OSes; `ports doctor` reports what's available and warns when a row is incomplete due to permissions.

---

## 2. Data model

```go
type Server struct {
    Port      int
    Proto     string        // tcp / tcp6
    PID, PPID int
    Cmdline   string
    Exe       string
    Cwd       string
    StartTime time.Time
    Uptime    time.Duration

    Project    *Project      // nil when not classified as "mine"
    Confidence int           // 0–100
}

type Project struct {
    Name        string
    Root        string        // repo root path
    Description string
    Marker      string        // which marker matched (".git", "package.json", …)
}
```

---

## 3. CLI surface

```
ports [list]              # table of your dev servers
  --all                   #   include system/standard ports too
  --json                  #   machine-readable
  --port <n>              #   filter to one port
  --watch                 #   refresh in place
  --sort port|uptime|name

ports kill <port|name>    # kill by port (3000) or project (nexxus)
  --force                 #   skip confirmation
  --timeout 5s            #   grace period before SIGKILL
  --single                #   kill only the listening PID, not the tree

ports tui                 # interactive table
ports doctor              # report per-OS capabilities, privileges, missing deps (lsof)
ports config              # show / edit config path
```

Config at `~/.config/ports/config.toml` (XDG; `%AppData%\ports` on Windows):
dev roots (default candidates `~/Development`, `~/dev`, `~/Projects`, `~/src`, `~/code`, `~/go/src`), ignore lists (ports / process names), kill timeout, confidence threshold.

---

## 4. Kill semantics

1. Resolve target → set of listening PIDs (one for a port; possibly several for a project).
2. For each, build the descendant tree via ppid walk (and process group where available).
3. `SIGTERM` the tree → wait `--timeout` → `SIGKILL` survivors.
4. Windows has no POSIX signals: `taskkill /T /F` (tree); graceful close is unreliable for non-console processes — documented as a known limitation.
5. Project-kill or multi-server kill prompts for confirmation unless `--force`.

---

## 5. TUI (Bubble Tea)

Table columns: **Port · Project · Description · Uptime · PID · marker**.

| Key | Action |
|-----|--------|
| ↑/↓ | move selection |
| `x` | kill selected (confirm modal `y/n`; shows terminating → killed) |
| `enter` | detail view: full cmdline, cwd, README description, child tree |
| `r` | refresh now (also auto-refreshes every N s) |
| `a` | toggle show-all |
| `/` | filter |
| `q` | quit |

---

## 6. Package layout

```
cmd/ports/main.go            # entrypoint; calls cli.Execute()
internal/cli/                # cobra command tree (list/kill/tui/config/doctor)
internal/model/              # shared Server / Project types
internal/scan/               # sockets + process enumeration
  scan.go
  cwd_linux.go               # build-tagged per OS
  cwd_darwin.go
  cwd_windows.go
internal/project/            # repo root, name, description
internal/classify/           # "is this mine?" scoring
internal/docker/             # compose detection path (labels → repo, k8s filtered)
internal/inventory/          # merge native + docker; shared View filter + Sort
internal/kill/               # tree kill + cross-platform signals
internal/config/             # config file, dev roots, ignore lists
internal/output/             # table / json rendering
internal/tui/                # bubbletea model
```

> Agent-facing docs: `AGENTS.md` (root) is the build/convention/invariant
> cheat-sheet, included by `CLAUDE.md`; `internal/scan` and `internal/kill` carry
> focused `AGENTS.md` notes for their per-OS and safety invariants.

---

## 7. Execution plan (phased)

- **Phase 0 — scaffold.** Go module, cobra skeleton, config loader, `Server`/`Project` types.
- **Phase 1 — discovery (the risk, done first).** Listening sockets + process info; **cwd resolution on all three OSes**; `ports list --all` showing raw rows; `ports doctor`.
- **Phase 2 — classification.** Repo-root walk, confidence scoring, project name + description extraction; **Docker/compose detection path** (labels → repo, k8s filtered out) merged + deduped with native results; filtered `ports list` and `--json`.
- **Phase 3 — kill.** Tree kill, graceful→force, per-OS signals; **`docker stop` backend for compose services**; `ports kill <port>` and `ports kill <name>` with confirmation.
- **Phase 4 — TUI.** Bubble Tea table, keybindings, confirm modal, auto-refresh, detail view.
- **Phase 5 — polish & distribution.** `--watch`, config UX, GoReleaser (darwin amd64/arm64, linux amd64/arm64, windows amd64), Homebrew tap + Scoop + `go install`, README, GitHub Actions CI.

---

## 8. Open risks

- macOS cwd depends on `lsof` (or optional cgo) — `ports doctor` flags if missing.
- Windows graceful shutdown is weaker than POSIX (tree-force only).
- Attribution must read the **tree**, not the leaf PID, or `npm`-wrapped servers get mislabeled.
- Same-user only without elevation: root-owned listeners (e.g. a native Postgres on `:5432`) show no PID to an unprivileged scan and appear under `--all` only, unattributed. `doctor` and the row flag this.
- macOS `lsof` may be a hard dependency for socket enumeration, not just cwd — confirm in the spike.
```

