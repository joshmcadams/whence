# Plan 017: TUI keymap — bindings and help text from one definition

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/tui README.md`
> Plans 009/013/014 land before this one and reshape parts of `tui.go`;
> treat their landed state as the base — the excerpts below are from
> `caec51a` and show SHAPES, not exact lines.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED (key handling is behavior; the Update-pumping tests catch regressions)
- **Depends on**: plans/009, plans/013, plans/014 (file-conflict ordering)
- **Category**: tech-debt
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Keybindings are raw string literals in a mode × key switch, and the footer
help is a separate hand-maintained string — a silent-drift lockstep pair that
has ALREADY drifted: the footer omits the confirm-mode keys (`s` toggle,
`y/N`) and the detail-mode keys. Adding or rebinding a key requires editing
the switch and remembering the help line(s). `bubbles/key` (already an
indirect part of the bubbles dependency) exists precisely for this: bindings
carry their own help text, and help renders from the same definitions the
handler matches on.

## Current state (shapes, at `caec51a`)

- `internal/tui/tui.go:198-289` — `handleKey`: `switch m.mode` then
  `switch msg.String()` over literals `"esc"`, `"enter"`, `"y"/"yes"`, `"s"`,
  `"q"`, `"r"`, `"a"`, `"t"`, `"/"`, `"x"`, `"ctrl+c"`.
- `internal/tui/tui.go:374-380` — `footerView`:

  ```go
  help := dimStyle.Render("↑/↓ move · x kill · enter details · / filter · a all · t theme · r refresh · q/esc quit")
  ```

- Confirm-mode help is inline in `confirmView` ("scope: … · s to toggle",
  `[y/N]`); detail-mode help inline in `detailView` ("esc back · q list").
- README's TUI key table (README.md:79-91) documents: ↑/↓, x, enter, /, a,
  t, r, q.
- Tests: `internal/tui/tui_test.go` drives keys via `Update` with
  `tea.KeyMsg` values — they pin BEHAVIOR per key and must pass unchanged.
- `github.com/charmbracelet/bubbles v1.0.0` is a direct dependency
  (`go.mod:8`); `bubbles/key` is importable without go.mod changes.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| TUI tests | `go test ./internal/tui/ -v` | pass |
| Suite | `make lint && make test` | exit 0 |
| Manual | `make run ARGS=tui` | keys work; footer lists them |

## Scope

**In scope**:
- `internal/tui/tui.go` (keymap struct, handler matching, footer render)
- A new `internal/tui/keys.go` (the keymap definition — keeps tui.go shrinking)
- `internal/tui/tui_test.go` (additions only; existing key tests unchanged)
- `README.md` TUI key table ONLY if a help string it quotes changes (it
  documents keys, not the footer string — likely untouched)

**Out of scope**:
- Changing ANY binding or adding new keys (plan 019 adds a sort key —
  that's why this plan should land first: 019 then adds one `key.Binding`).
- Splitting view functions into `view.go` — optional in the audit; skip it
  here to keep the diff reviewable (note as deferred).
- The bubbles `help` widget — overkill; the footer stays a one-liner built
  from the bindings.

## Git workflow

- Branch: `advisor/017-tui-keymap`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Define the keymap

New `internal/tui/keys.go`:

```go
import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
    Up, Down                    key.Binding // table navigation (help only; table handles them)
    Kill, Detail, Filter        key.Binding
    All, Theme, Refresh, Quit   key.Binding
    ConfirmYes, ConfirmScope, ConfirmNo key.Binding
    DetailBack                  key.Binding
    FilterApply, FilterCancel   key.Binding
}

func defaultKeyMap() keyMap {
    return keyMap{
        Kill:   key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "kill")),
        Detail: key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
        Filter: key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
        All:    key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all")),
        Theme:  key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "theme")),
        Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
        Quit:   key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q/esc", "quit")),
        Up:     key.NewBinding(key.WithKeys("up"), key.WithHelp("↑/↓", "move")),
        ConfirmYes:   key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "confirm")),
        ConfirmScope: key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "toggle scope")),
        ConfirmNo:    key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "cancel")),
        DetailBack:   key.NewBinding(key.WithKeys("esc", "enter", "q"), key.WithHelp("esc", "back")),
        FilterApply:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
        FilterCancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
    }
}
```

EXACT key sets must reproduce the current switch — read the live `handleKey`
and transcribe every accepted key per mode (e.g. confirm-mode "yes" full word:
current code lowercases `msg.String()` and matches `"y", "yes"` — carry both).
Add the map to `Model` (`keys keyMap`, set in `New`).

### Step 2: Match on bindings

Rewrite `handleKey`'s cases from string literals to
`key.Matches(msg, m.keys.X)`. Preserve structure and order exactly — mode
switch outer, per-mode inner; the confirm-mode default-cancels behavior
(`default: → modeList`) stays a default, with `ConfirmNo` existing for help
text. The `strings.ToLower` normalization in confirm mode must be kept (or
encode `Y`/`yes` variants in the binding keys — pick one, keep behavior).

**Verify**: `go test ./internal/tui/ -v` → every pre-existing key test passes
UNCHANGED. This is the gate that proves no binding drifted.

### Step 3: Help from the bindings

Replace the hand-written footer string with a builder:

```go
func (m Model) helpLine() string // joins " · "-separated "key desc" pairs per mode
```

- modeList: Up(↑/↓ move), Kill, Detail, Filter, All, Theme, Refresh, Quit —
  reproducing today's footer content from binding help fields.
- modeConfirm: ConfirmYes, ConfirmScope (only when the scope toggle is
  applicable — same condition `confirmView` uses today), ConfirmNo.
- modeDetail: DetailBack.
- modeFilter: FilterApply, FilterCancel.

`footerView` renders `helpLine()` for the current mode. `confirmView`'s
inline "s to toggle" hint and `detailView`'s "esc back · q list" line are
DELETED in favor of the footer (single source). Add a test: for each mode,
`View()` contains the help text of that mode's bindings (e.g. confirm mode
shows "toggle scope"; list mode shows "kill").

**Verify**: `go test ./internal/tui/ -v` → pass (update only tests that
pinned the deleted inline hint strings — list them in the report);
manual `make run ARGS=tui`: footer changes per mode, all keys still work.

### Step 4: Gate

**Verify**: `make lint && make test` → exit 0. Cross-check README's key
table still matches the bindings (it should — no binding changed).

## Test plan

Step 2's unchanged-tests gate + Step 3's per-mode help assertions. Pattern:
existing Update-pumping tests.

## Done criteria

- [ ] `grep -c '"x"\|"/"\|"t"' internal/tui/tui.go` shows no raw key literals left in handleKey (they live in keys.go)
- [ ] Footer/help text is generated from bindings; per-mode help tests pass
- [ ] ALL pre-existing key-behavior tests pass without modification
- [ ] `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- Any pre-existing key test requires a behavior-level change to pass — a
  binding got dropped in transcription; fix the transcription, and if it
  still fails, report.
- `bubbles/key` at v1.0.0 lacks an API this plan assumes (`key.NewBinding`,
  `key.Matches`, `WithKeys`, `WithHelp`) — check
  `~/go/pkg/mod/github.com/charmbracelet/bubbles@v1.0.0/key/key.go` and
  report if the shapes differ.

## Maintenance notes

- Plan 019 adds its sort key as one `key.Binding` + one help entry — the
  payoff of this plan; reviewers should reject future raw-literal keys.
- Plan 022 (bubbletea v2) changes `tea.KeyMsg`; `bubbles/v2/key` has the
  matching API — the keymap structure survives, the imports change.
- Deferred: splitting `view.go` out of `tui.go` (audit suggestion) — do it
  opportunistically in plan 022's migration if churn is already total.
