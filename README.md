# whence

Find the dev servers and databases **you** are running on local ports, map each
back to the repo it was launched from, and kill them by port or by project —
from the command line or an interactive TUI.

`whence` ignores system and standard-application ports and focuses on things you
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
- **Kill by port or project** — `whence kill 3000` or `whence kill myapp`. Native processes are killed as a tree (graceful `SIGTERM`, then `SIGKILL`) without touching your shell; compose services are stopped via `docker stop`.
- **Interactive TUI** — arrow-key navigation, `x` to kill, `enter` for details, live auto-refresh.

## Install

> The module path and release owner currently use `joshmcadams`. If your GitHub
> username/repo differ, update the module path in `go.mod`, the `-X` ldflags
> path, and the owners in `.goreleaser.yaml` before releasing.

**From source** (Go 1.25+):

```sh
go install github.com/joshmcadams/whence/cmd/whence@latest
```

**Prebuilt binaries:** download from the
[releases page](https://github.com/joshmcadams/whence/releases).

**Homebrew** (after a release):

```sh
brew install --cask joshmcadams/tap/whence
```

**Scoop** (Windows):

```sh
scoop bucket add joshmcadams https://github.com/joshmcadams/scoop-bucket
scoop install whence
```

## Usage

```sh
whence                      # list your dev servers (alias for `whence list`)
whence list --all           # include every listening port
whence list --json          # machine-readable output
whence list --port 3000     # only this port
whence list --sort uptime   # sort by port|uptime|name
whence list --watch         # live-refresh in place (Ctrl-C to stop)
whence list --watch --interval 5s   # change the --watch refresh interval (default 2s)
whence list --no-ignore     # bypass ignore_ports / ignore_names

whence kill 3000            # kill the server on a port
whence kill myapp           # kill every server in a project (exact name preferred)
whence kill 3000 --force    # skip the confirmation prompt
whence kill 3000 --single   # kill only the listening process, not its tree
whence kill 3000 --timeout 10s   # grace period before SIGKILL (default from config)

whence tui                  # interactive table
whence tui --all            # start the TUI showing all ports, not just yours

whence doctor               # platform capabilities & diagnostics
whence config               # show effective config
whence config --init        # write a default config file
```

### TUI keys

| Key | Action |
|-----|--------|
| `↑` / `↓` | move selection |
| `x` | kill selected (with confirmation) |
| `enter` | detail view |
| `/` | filter |
| `a` | toggle all / yours |
| `t` | cycle color theme (saved to config) |
| `r` | refresh now |
| `q` | quit |

Themes: `indigo` (default), `teal`, `amber`, `magenta`, `green`, `mono`
(terminal reverse-video). Cycle with `t` in the TUI — the choice is written back
to your config — or set `theme` in the config file directly.

## Configuration

Config lives at `~/.config/whence/config.toml` (XDG) or `%AppData%\whence\config.toml`
on Windows. Run `whence config --init` to scaffold it.

```toml
dev_roots = ["/home/you/development", "/home/you/dev", "/home/you/go/src"]
# default also includes ~/Projects, ~/projects, ~/src, ~/code, ~/Code, ~/work
ignore_ports = []
ignore_names = []
kill_timeout_seconds = 5
confidence_threshold = 50
theme = "indigo"   # indigo | teal | amber | magenta | green | mono
```

A server is shown by default when its confidence ≥ `confidence_threshold`. Use
`--all` to see everything.

`ignore_ports` and `ignore_names` suppress matching servers from every listing —
**including `--all`** — which is the point: system noise (a root-owned Postgres,
`docker-proxy`) typically only appears under `--all`. `ignore_names` matches a
process/container name or a project name (case-insensitive). To peek past the
lists, `whence list --port <n>` still shows a specific ignored port, and
`whence list --no-ignore` bypasses them entirely. `whence doctor` prints the
active lists. (Ignore lists never block `whence kill` — an explicit kill always
targets what you name.)

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
- `whence` reports the machine it runs on. Inside WSL it sees the Linux side; on
  the Windows host it sees Windows.
- macOS requires `lsof` (preinstalled) for working-directory resolution.
- On Windows, graceful shutdown is best-effort (`taskkill`); a tree force-kill is
  always available.

## Development

```sh
go test ./...
go build ./cmd/whence
```

## License

MIT — see [LICENSE](LICENSE).
