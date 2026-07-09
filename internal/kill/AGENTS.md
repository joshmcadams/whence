# AGENTS.md — `internal/kill`

This package destroys processes. The invariants here are safety-critical: a bug
can take down the user's terminal or kill more than they asked for. Read this
before changing anything.

## Invariant 1 — never kill the shell or init

`climb(pid)` walks *up* from the listening PID through known launcher wrappers to
the tree head, then `subtree` kills that head and all descendants. The climb
**must stop**:

- at any parent **not** in the `launchers` allow-list (notably shells:
  bash/zsh/sh/fish/pwsh/cmd are deliberately absent), and
- at pid ≤ 1 (init).

So an interactive session is never a kill target. If you edit `launchers`, keep
shells out, and keep **bare `node` out** — including it lets a kill climb into a
long-lived node host (e.g. an editor's extension host).

In practice `npm run <script>` / `yarn` / `pnpm` execute the script body via an
interposed `sh -c` (`npm → sh → node`), and many `make` recipes do the same for
any line with shell metacharacters. Since shells are deliberately not
launchers, `climb` stops at that `sh` and never reaches `npm`/`make` above it —
the listening `node` (or its immediate non-shell parent) is the head of the
kill, and the wrapper above the shell is untouched. `npm`/`yarn`/`pnpm`/`make`
are still in the `launchers` list for the other case: a wrapper that execs the
server **directly**, with no intervening shell (e.g. `npm exec <bin>` on some
platforms, or a `make` recipe with a single non-shell command) — there, climb
does walk up into the wrapper as designed. Either way the confirmation preview
computes the same tree the kill will act on (see below), so what you see is
what dies — never guess from this paragraph alone. There are tests
(`kill_test.go`) for `climb`/`subtree`; extend them when you touch the list.

## Invariant 2 — graceful then forced

`SIGTERM` the whole tree → poll until the timeout → `SIGKILL` survivors. Signals
are build-tagged: `signal_unix.go` (`syscall.Kill`) and `signal_windows.go`
(`taskkill`, where graceful close is best-effort — documented limitation). Don't
inline a `runtime.GOOS` branch; use the per-OS file.

## Invariant 3 — docker is a separate backend

`Source == SourceDocker` servers are stopped with `docker stop -t`, never a host
signal. `Single` does not apply to them.

## Preview and kill must agree

A kill climbs to a launcher and takes the whole subtree, so one listening server
can mean several processes — and a shared launcher (`make dev` / one `npm` script
starting multiple services) means `whence kill <port>` stops the siblings too.

So that the confirmation can't lie about the blast radius, the scope is computed
in exactly one place — `planTree(pid, single, tbl)` — which both `killProcess`
(the action) and `Preview` (the confirmation in `cli/kill.go`) call. If you
change how scope is resolved, change `planTree`; never recompute the tree
separately in one path, or the prompt and the kill will drift apart.

## Identity re-check, cycle guards, zombie-aware liveness

Before signaling anything, `killProcess` compares the scanned PID's OS-reported
create time against `Server.StartTime` (±2s, via `verifyIdentity`) and refuses
with "target changed since scan" on mismatch or an unreadable create time —
nothing is signaled on refusal. A zero `StartTime` skips the check (nothing to
compare against). `climb`/`subtree` carry a visited-set guard so a ppid/child
cycle in the process table can't hang a kill or double-count a pid. `isAlive`
treats a zombie (`process.Status() == process.Zombie`) as dead so a killed-but-
not-yet-reaped child doesn't burn the full grace period as a false survivor.
