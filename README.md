# ports

Find the dev servers and databases **you** are running on local ports, map each
back to the repo it was launched from, and kill them by port or by project —
from the command line or an interactive TUI.

`ports` ignores system and standard-application ports and focuses on things you
are actively developing: servers started with `npm run dev`, `make dev`,
`go run`, `flask run`, a `docker compose` database, and so on. For each it shows
the **port, project name, uptime, and a description** pulled from the project's
README or package metadata.

```
PORT  PROTO  PID     UPTIME  SRC     SERVER  DESCRIPTION
5173  tcp    351638  7h7m    proc    jfdid   Multi-user task/todo system with context-aware…
5433  tcp    -       15h55m  docker  jfdid   Multi-user task/todo system with context-aware…
8080  tcp    -       15h55m  docker  jfdid   Multi-user task/todo system with context-aware…
```

## Features

- **Cross-platform** — single static binary for macOS, Linux, and Windows (incl. WSL).
- **Knows what's yours** — a confidence score from your configured dev roots, repo markers (`.git`, `package.json`, `go.mod`, …), and dev-server-looking commands.
- **Understands Docker Compose** — attributes containers to their repo via compose labels; Kubernetes-managed containers are filtered out.
- **Kill by port or project** — `ports kill 3000` or `ports kill myapp`. Native processes are killed as a tree (graceful `SIGTERM`, then `SIGKILL`) without touching your shell; compose services are stopped via `docker stop`.
- **Interactive TUI** — arrow-key navigation, `x` to kill, `enter` for details, live auto-refresh.

## Install

> The module path and release owner currently use `jmcadams`. If your GitHub
> username/repo differ, update the module path in `go.mod`, the `-X` ldflags
> path, and the owners in `.goreleaser.yaml` before releasing.

**From source** (Go 1.26+):

```sh
go install github.com/jmcadams/ports/cmd/ports@latest
```

**Prebuilt binaries:** download from the
[releases page](https://github.com/jmcadams/ports/releases).

**Homebrew** (after a release):

```sh
brew install --cask jmcadams/tap/ports
```

**Scoop** (Windows):

```sh
scoop bucket add jmcadams https://github.com/jmcadams/scoop-bucket
scoop install ports
```

## Usage

```sh
ports                       # list your dev servers (alias for `ports list`)
ports list --all            # include every listening port
ports list --json           # machine-readable output
ports list --port 3000      # only this port
ports list --sort uptime    # sort by port|uptime|name
ports list --watch          # live-refresh in place (Ctrl-C to stop)

ports kill 3000             # kill the server on a port
ports kill myapp            # kill every server in a project
ports kill 3000 --force     # skip the confirmation prompt
ports kill 3000 --single    # kill only the listening process, not its tree

ports tui                   # interactive table

ports doctor                # platform capabilities & diagnostics
ports config                # show effective config
ports config --init         # write a default config file
```

### TUI keys

| Key | Action |
|-----|--------|
| `↑` / `↓` | move selection |
| `x` | kill selected (with confirmation) |
| `enter` | detail view |
| `/` | filter |
| `a` | toggle all / yours |
| `r` | refresh now |
| `q` | quit |

## Configuration

Config lives at `~/.config/ports/config.toml` (XDG) or `%AppData%\ports\config.toml`
on Windows. Run `ports config --init` to scaffold it.

```toml
dev_roots = ["/home/you/development", "/home/you/dev", "/home/you/go/src"]
ignore_ports = []
ignore_names = []
kill_timeout_seconds = 5
confidence_threshold = 50
```

A server is shown by default when its confidence ≥ `confidence_threshold`. Use
`--all` to see everything.

## How it works

1. Enumerate listening TCP sockets and the owning PID.
2. Resolve each process's working directory — `/proc/<pid>/cwd` on Linux/WSL,
   `lsof` on macOS, the PEB on Windows.
3. Walk up to the repo root (nearest `.git`, then a manifest) for name + description.
4. Score how likely the server is "yours" and attach the project.
5. In parallel, query Docker for published ports and attribute compose services
   to their repo via the `com.docker.compose.project.working_dir` label.

### Notes & limitations

- Without elevated privileges you only see your own processes; root-owned native
  listeners (e.g. a system Postgres) appear under `--all` without attribution.
  Run with `sudo` to attribute them, or rely on the Docker path for containers.
- `ports` reports the machine it runs on. Inside WSL it sees the Linux side; on
  the Windows host it sees Windows.
- macOS requires `lsof` (preinstalled) for working-directory resolution.
- On Windows, graceful shutdown is best-effort (`taskkill`); a tree force-kill is
  always available.

## Development

```sh
go test ./...
go build ./cmd/ports
```

## License

MIT — see [LICENSE](LICENSE).
