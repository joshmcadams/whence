# Plan 003: Sanitize untrusted strings at every terminal render boundary

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/output internal/cli/kill.go internal/tui/tui.go internal/model`
> On any in-scope drift, compare the excerpts below to live code first.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW (additive filtering of non-printable runes)
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Every human-readable output path prints untrusted strings raw: process exe
basenames and cmdlines (any process the user's account runs — e.g. something
started by a malicious npm postinstall), `package.json`/`Cargo.toml`
name/description fields and README first lines of arbitrary repos on disk,
and docker image/label values. Embedded ANSI/OSC escape sequences can rewrite
or hide rows in `whence list` and — worst case — visually falsify the kill
confirmation prompt, the product's core safety mechanism. Some terminals also
honor OSC sequences with side effects (title set, OSC 52 clipboard write).
The JSON path is already safe (Go's encoder escapes control bytes); the human
paths are not.

## Current state

Untrusted strings reach the terminal at these places:

- `internal/output/output.go:50-66` — `Table` prints `s.DisplayName()`,
  `s.Description()` (via `Truncate`), and `s.Proto` raw with `fmt.Fprintf`.
- `internal/output/output.go:71-76` — `note(s)` prints `s.Notes[0]` raw
  (notes embed error strings that can contain process-controlled text).
- `internal/cli/kill.go:190-208` — `printPlan`/`describe` print
  `p.Lines()` (which embed `TreeMember.Name`, a process name) and
  `s.DisplayName()`/`s.Name` raw.
- `internal/kill/kill.go:100-121` — `Plan.Lines()` builds those strings from
  `m.Name` (do NOT sanitize here; sanitize at render, see design note).
- `internal/tui/tui.go:315-327` — `rebuild()` builds table rows from
  `DisplayName()`/`Description()`.
- `internal/tui/tui.go:395-422` — `confirmView` renders `describe(m.selected)`
  and `p.Lines()`.
- `internal/tui/tui.go:425-458` — `detailView` renders `s.Name`, `s.Cmdline`,
  `s.Exe`, `s.Cwd`, `DisplayName()`, `Description()`, `Project.Root`.
- `internal/tui/tui.go:363-371` — `headerView` renders `m.query` (user-typed,
  low risk) and `m.err.Error()` (can embed scan-derived text).
- `internal/tui/tui.go:176-178` and `internal/cli/kill.go:89` — kill status
  lines embed `describe(...)` and error text.

There is no sanitization anywhere in the repo (verified by reading all render
paths). Design note: sanitize at the **render boundary**, not in `model` or
`kill` — the JSON output must keep the raw values (machine consumers may want
them), and sanitizing storage would hide data from `--json`.

Repo conventions: `internal/output` is the rendering helper package both CLI
and TUI already import (`output.HumanUptime`, `output.Truncate` are used from
`internal/tui/tui.go:323-326`), so the sanitizer lives there. White-box tests
in the same package; see `internal/output/output_test.go` for the existing
table-driven style.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Build | `go build ./...` | exit 0 |
| Output tests | `go test ./internal/output/ -v` | pass |
| Full suite | `make test` | ok |
| Lint | `make lint` | exit 0 |

## Scope

**In scope**:
- `internal/output/output.go`, `internal/output/output_test.go`
- `internal/cli/kill.go` (apply sanitizer in `printPlan`/`describe`)
- `internal/tui/tui.go` (apply sanitizer in `rebuild`, `confirmView`,
  `detailView`, `headerView`, and the `killedMsg` status lines)
- `internal/cli/kill_test.go`, `internal/tui/tui_test.go` (one test each)

**Out of scope**:
- `output.JSON` — must keep raw values; do not sanitize.
- `internal/model`, `internal/kill`, `internal/scan` — no sanitizing at the
  data layer.
- Stripping pre-existing ANSI styling that lipgloss adds — the sanitizer runs
  on *content* before styling, never on styled output.

## Git workflow

- Branch: `advisor/003-terminal-sanitize`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Add `output.Sanitize`

In `internal/output/output.go`:

```go
// Sanitize makes an untrusted string safe to print to a terminal: C0 control
// characters (except tab), DEL, and C1 control characters (0x80–0x9F, the
// range that encodes CSI/OSC in 8-bit form) are replaced with '?'. Newlines
// are replaced too — every render site here is single-line. Content is
// sanitized at the render boundary only; JSON output keeps raw values.
func Sanitize(s string) string {
    // Fast path: scan for offenders before allocating.
    clean := true
    for _, r := range s {
        if isUnsafeRune(r) { clean = false; break }
    }
    if clean { return s }
    var b strings.Builder
    b.Grow(len(s))
    for _, r := range s {
        if isUnsafeRune(r) {
            b.WriteRune('?')
        } else {
            b.WriteRune(r)
        }
    }
    return b.String()
}

func isUnsafeRune(r rune) bool {
    return (r < 0x20 && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f)
}
```

Keep tab allowed ONLY if it doesn't break the tabwriter table — it does
(tabwriter treats tabs as column separators), so for this repo replace tab as
well: drop the `r != '\t'` exception. Final predicate:
`r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f)`.

**Verify**: `go build ./...` → exit 0.

### Step 2: Apply at the CLI render boundaries

- `output.Table`: wrap `name` (after the `[!]` append is fine — apply to
  `s.DisplayName()` result before appending), `desc`, `s.Proto`, and the
  `note(s)` return value in `Sanitize`.
- `internal/cli/kill.go`: in `describe`, sanitize `name` and `s.Name`; in
  `printPlan`, sanitize each `line` from `p.Lines()`.

### Step 3: Apply at the TUI render boundaries

In `internal/tui/tui.go`:
- `rebuild`: sanitize `name` and the description argument.
- `describe` (tui version, lines 486-495): sanitize `name`.
- `confirmView`: sanitize each `line` from `p.Lines()`.
- `detailView`: sanitize every value passed to `row(...)` that originates in
  scan/docker/project data (`s.Name`, `s.Cmdline`, `s.Exe`, `s.Cwd`,
  `DisplayName()`, `Project.Root`, `Project.Marker`) and the wrapped
  `s.Description()`.
- `headerView`: sanitize `m.err.Error()` output.
- `killedMsg` handler: the status strings embed `describe(...)` (already
  sanitized by the describe change) and `msg.res.Err.Error()` — sanitize the
  error text.

The simplest reviewable shape: a tiny local alias `san := output.Sanitize`
is fine, but do not create a second sanitizer implementation.

**Verify**: `go build ./... && go test ./internal/tui/` → pass.

### Step 4: Tests

- `internal/output/output_test.go`: table-driven `TestSanitize` — cases:
  plain ASCII unchanged; string with `\x1b]0;evil\x07` (OSC title set) has no
  0x1b/0x07 left; `\x1b[2K\r` (line erase) neutralized; C1 byte 0x9b
  neutralized; multi-byte UTF-8 (e.g. `"café — ✓"`) passes through unchanged;
  embedded `\n`/`\t` replaced. Plus one `Table` test: a server whose
  DisplayName contains `\x1b[8m` renders a table containing no `\x1b`.
- `internal/cli/kill_test.go`: reuse plan 001's `runKillWith` harness (if
  landed) or test `describe` directly: a server named `"web\x1b[1A"` renders
  with no escape byte.
- `internal/tui/tui_test.go`: pump a `loadedMsg` containing a server with an
  escape in its description; assert `m.View()` (or the built rows) contains
  no `\x1b` **originating from content** — note the View itself contains
  lipgloss styling escapes, so assert on the *row cell strings* via the
  rebuilt table rows rather than the full rendered View.

**Verify**: `make lint && make test` → exit 0.

## Done criteria

- [ ] `make lint` and `make test` exit 0
- [ ] `grep -n "Sanitize" internal/output/output.go internal/cli/kill.go internal/tui/tui.go` shows applications at Table, describe (both), printPlan, rebuild, confirmView, detailView
- [ ] `output.JSON` is unmodified (`git diff caec51a..HEAD -- internal/output | grep -A3 "func JSON"` shows no change)
- [ ] New sanitizer tests pass, including the OSC and C1 cases
- [ ] No files outside the in-scope list modified (`git status`)
- [ ] `plans/README.md` status row updated

## STOP conditions

- The tabwriter output visibly breaks (column misalignment in a manual
  `make run ARGS="list --all"`) after sanitizing — report before changing the
  replacement strategy.
- You find yourself wanting to sanitize inside `internal/model` or
  `internal/kill` — that changes `--json` output; stop and report.
- Existing tests pin a string that contains a control character (unexpected —
  would mean the fixture itself is odd).

## Maintenance notes

- Plan 014 consolidates the CLI/TUI row rendering into shared helpers; when it
  lands, the sanitizer applications collapse into the shared builder — 014's
  plan says so. A reviewer of 014 must check the sanitizer survives the merge.
- Any NEW render path (e.g. plan 019's detail-view tree) must route its
  content through `output.Sanitize`; the plans that add such paths say so, but
  it's the thing to watch in review.
- Deliberately NOT stripped: printable Unicode (including RTL/zero-width) —
  overkill for a local dev tool; revisit only if a concrete spoof via
  zero-width chars is demonstrated.
