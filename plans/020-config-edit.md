# Plan 020: `whence config --edit`

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/cli/config.go internal/execx internal/config README.md AGENTS.md`

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW-MED (interactive child process; Windows is the least-exercised platform)
- **Depends on**: none
- **Category**: direction
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

DESIGN.md's CLI spec promised "`whence config` — show / **edit** config
path"; only show and `--init` shipped. Today every non-theme config change is
"run `whence config` to find the path, open it yourself" — for a
confidence-scored tool, tuning `dev_roots` is the single most common config
act and it's all by hand. The save path is already proven (the TUI persists
theme changes via `config.Save`).

## Current state

- `internal/cli/config.go:13-45` — `newConfigCmd`: prints the effective
  config (path, dev roots, ignore lists, timeout, threshold, theme) and
  supports `--init` (writes `config.Default()` via `config.Save`, refusing
  when the file exists).
- `internal/config/config.go:57-69` — `Path()` (XDG / AppData). `Save`
  creates the parent dir. `Load` tolerates a missing file.
- `internal/execx/execx.go` — the repo's only sanctioned shell-out wrapper;
  everything runs with a hard timeout and captured output. An interactive
  editor needs the opposite: inherited stdio and NO timeout — a new,
  clearly-scoped primitive rather than a rule violation.
- AGENTS.md convention: "**every shell-out goes through it** [execx], never
  `os/exec` directly". This plan extends execx rather than bypassing it.
- Editor conventions: `$VISUAL` then `$EDITOR`; fallbacks `vi` (unix) /
  `notepad` (windows). Per-OS code is build-tagged, one file per OS.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Suite | `make lint && make test` | exit 0 |
| Windows compile | `GOOS=windows GOARCH=amd64 go build ./...` | exit 0 |
| Manual | `EDITOR=true make run ARGS="config --edit"` | exits 0 after "editing" |
| Manual 2 | `EDITOR="" VISUAL="" make run ARGS="config --edit"` | uses vi or errors helpfully |

## Scope

**In scope**:
- `internal/execx/execx.go` (+ `execx_test.go`): one `Interactive` function
- New `internal/cli/editor_unix.go` / `internal/cli/editor_windows.go`
  (fallback editor name) — or put the fallback in `internal/execx`; keep it
  wherever the build-tag layout reads most naturally, but per-OS files, no
  GOOS branches
- `internal/cli/config.go` (+ a test file)
- `README.md` (config section: one line), `AGENTS.md` (one sentence
  documenting the interactive carve-out)

**Out of scope**:
- Validating the config the user saves (Load already tolerates/reports TOML
  errors on next run).
- Any TUI config editing.
- `$EDITOR` values with arguments (e.g. `code --wait`) — v1 treats the value
  as a single binary; document the limitation in the flag help. STOP rather
  than improvise shell-splitting.

## Git workflow

- Branch: `advisor/020-config-edit`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: `execx.Interactive`

```go
// Interactive runs a command wired to the caller's terminal (stdin/stdout/
// stderr inherited) and waits for it to exit. Unlike Output/CombinedOutput
// there is deliberately NO timeout: the child is an interactive program
// (e.g. $EDITOR) whose lifetime the user controls. Use only for
// user-launched interactive children.
func Interactive(name string, args ...string) error {
    cmd := exec.Command(name, args...)
    cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
    return cmd.Run()
}
```

Test (pattern: `execx_test.go`'s real-process style): `Interactive("true")`
→ nil; `Interactive("false")` → non-nil exit error;
`Interactive("definitely-not-a-binary-xyz")` → non-nil. (Stdio inheritance
is not assertable in a unit test; the manual smoke covers it.)

**Verify**: `go test ./internal/execx/ -v` → pass.

### Step 2: Editor resolution (per-OS fallback)

`internal/cli/editor_unix.go` (`//go:build !windows`):
`func fallbackEditor() string { return "vi" }`;
`editor_windows.go`: `return "notepad"`. Shared resolution in `config.go`:

```go
func resolveEditor() string {
    for _, v := range []string{os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
        if strings.TrimSpace(v) != "" { return v }
    }
    return fallbackEditor()
}
```

### Step 3: The `--edit` flag

In `newConfigCmd`: add `--edit` (bool). Behavior:

1. If the config file doesn't exist, write defaults first (reuse the exact
   `--init` code path, minus its "already exists" refusal), printing the
   "created <path>" line it prints today.
2. Resolve the editor; `execx.Interactive(editor, config.Path())`.
3. On editor exit 0: reload via `config.Load()` and print the effective
   config (the command's existing output) so the user sees what took effect;
   a TOML error from Load prints the error and the path (non-zero exit).
4. `--edit` and `--init` together: error (`choose one`).

Flag help: `open the config file in $VISUAL/$EDITOR (single binary name; falls back to vi/notepad)`.

Tests (white-box; `XDG_CONFIG_HOME` temp-dir isolation like
`internal/config/config_test.go`): with `EDITOR=true` — file gets created
when absent and the run returns nil; `EDITOR=false` → non-nil error;
`--edit --init` → error. (Set env with `t.Setenv`.)

**Verify**: `go test ./internal/cli/ -v` → pass.

### Step 4: Docs

- README config section: add `whence config --edit   # open in $EDITOR`.
- AGENTS.md execx convention sentence: append "…; `execx.Interactive` is the
  sanctioned form for user-launched interactive children (no timeout,
  inherited stdio) — everything else keeps the timeout rule."

**Verify**: `make lint && make test` → exit 0;
`GOOS=windows GOARCH=amd64 go build ./...` → exit 0; both manual smokes.

## Test plan

Steps 1 and 3; patterns: `execx_test.go` real processes,
`config_test.go` env isolation.

## Done criteria

- [ ] `execx.Interactive` exists, documented as the no-timeout carve-out, with tests
- [ ] `whence config --edit` creates-if-missing, launches the editor, re-prints effective config
- [ ] `grep -rn "os/exec" internal/cli/` → no direct use (editor goes through execx)
- [ ] AGENTS.md documents the carve-out; README documents the flag
- [ ] `make lint && make test` + windows cross-compile exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- Supporting `$EDITOR` with embedded arguments turns out to be required for
  your validation (e.g. only `code --wait` available) — don't shell-split;
  report the design question.
- The `--init` reuse can't avoid duplicating its logic — small duplication is
  acceptable; gutting `--init`'s refusal semantics is not.

## Maintenance notes

- If `$EDITOR`-with-args support is added later, use proper argv splitting
  (e.g. `shlex`-style), never `sh -c` with the path interpolated.
- The "reload and print" step doubles as the user's validation feedback; if
  config validation is ever added to `Load`, this command inherits it free.
