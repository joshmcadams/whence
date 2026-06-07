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
long-lived node host (e.g. an editor's extension host). The npm/yarn/pnpm chain
is still handled: we climb through those wrappers and any `node` helper dies as a
*descendant* of the subtree, never as a climb target. There are tests
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
starting multiple services) means `ports kill <port>` stops the siblings too.

So that the confirmation can't lie about the blast radius, the scope is computed
in exactly one place — `planTree(pid, single, tbl)` — which both `killProcess`
(the action) and `Preview` (the confirmation in `cli/kill.go`) call. If you
change how scope is resolved, change `planTree`; never recompute the tree
separately in one path, or the prompt and the kill will drift apart.
