# imp-03 — Implement (or remove) `ignore_ports` / `ignore_names`

**Status:** done (merged to main)
**Priority:** high — a documented, configurable feature that silently does nothing
**Category:** correctness / trust
**Effort:** ~30 min to implement; ~5 min to remove

> **Implemented** (Option A) in the shared `inventory.View` filter via a new
> `isIgnored` helper, so the CLI and TUI honor the lists identically.
> `IgnoreNames` matches a process/container name or project name,
> case-insensitive and **exact** (chosen over substring so suppression is
> predictable). Semantics confirmed deliberately: ignored entries are hidden
> **even with `--all`** — that's the point, since the system noise users want to
> mute (a root-owned Postgres, `docker-proxy`) only surfaces under `--all`.
> Escape hatches so it isn't a silent footgun: an explicit `whence list
> --port <n>` still shows an ignored port, `whence list --no-ignore` bypasses the
> lists entirely (clears them on a cfg value-copy — no signature churn in
> `View`), and `whence doctor` now prints the active lists. `whence kill` uses
> the unfiltered inventory, so ignores never block an explicit kill. Tested in
> `inventory_test.go` (`TestView_IgnorePorts`, `TestView_IgnoreNames`) and
> smoke-verified end-to-end.

## Problem

`config.Config` defines two ignore lists, with comments promising behavior:

```go
// internal/config/config.go:19
// IgnorePorts are never shown even with --all.
IgnorePorts []int `toml:"ignore_ports"`
// IgnoreNames are process names to suppress.
IgnoreNames []string `toml:"ignore_names"`
```

They appear in `config.Default()`, in `whence config` output, in the README's
config example, and in `DESIGN.md` ("ignore lists (ports / process names)").

But `grep -rn "IgnorePorts\|IgnoreNames"` shows they are **only ever
declared** — never read. `inventory.View` and `classify.Process` don't consult
them. A user who sets:

```toml
ignore_ports = [5432]
ignore_names = ["docker-proxy"]
```

sees exactly no change. The config silently lies.

## Why it matters

This is worse than a missing feature: it's a feature the tool *advertises in its
own output* (`whence config` prints these keys) and then ignores. The first time
a user tries to quiet a noisy port and it doesn't work, they stop trusting the
config file. Cheap to fix, disproportionate trust cost if left.

## Suggested approach

**Option A — implement (preferred).** The natural seam is `inventory.View`
(`internal/inventory/inventory.go:49`), which already centralizes display
filtering for both the CLI and the TUI:

```go
for _, s := range servers {
    if containsInt(cfg.IgnorePorts, s.Port) {
        continue
    }
    if matchesIgnoredName(cfg.IgnoreNames, s) { // check Name and DisplayName, case-insensitive
        continue
    }
    // …existing port / confidence / query filters…
}
```

Decisions to make explicit (and document):
- The comment says ignored ports are hidden *even with `--all`*. Honor that:
  apply ignore filtering before the `all` check, so `--all` still respects it.
  (Add a `--no-ignore` escape hatch if you want a way to see everything.)
- Match `IgnoreNames` against both the process/container `Name` and the project
  `DisplayName`, case-insensitively, to match user expectations.

**Option B — remove.** If ignore lists aren't wanted, delete the fields, drop
them from `Default()`, the README config block, and the DESIGN CLI/config
section. Don't leave dead, documented knobs.

## Tests / verification

- `internal/inventory` already has `View` tests (`inventory_test.go`). Add:
  a server on an ignored port is absent even when `all=true`; a server whose
  name is in `IgnoreNames` is filtered; non-ignored servers are untouched.
- Manual: set `ignore_ports = [<a real listening port>]`, run `whence list
  --all`, confirm it's gone.

## Notes / trade-offs

- Putting the filter in `View` keeps CLI and TUI consistent for free (both call
  it) — the same reason the confidence filter lives there.
- This is the only place in the codebase where config and behavior disagree;
  closing it removes a latent "why doesn't this work?" support question.
