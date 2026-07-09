# Plan 017: TUI keymap — bindings and help text from one definition

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 76befde..HEAD -- internal/tui README.md`
> Plans 009, 013, and 014 are DONE and have reshaped `tui.go`. The excerpts
> below are from the post-014 state at `76befde`. If any in-scope file
> changed since, compare excerpts against live code; on a mismatch, treat it
> as a STOP condition.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED (key handling is behavior; the Update-pumping tests catch regressions)
- **Depends on**: plans/009, plans/013, plans/014
- **Category**: tech-debt
- **Planned at**: commit `76befde`, 2026-07-09

## Why this matters

Keybindings are raw string literals in a mode × key switch, and the footer
help is a separate hand-maintained string — a silent-drift lockstep pair that
has already drifted: the footer omits confirm-mode keys (`s` toggle, `y/N`)
and detail-mode keys. Adding or rebinding a key requires editing the switch
AND remembering the help line(s). `bubbles/key` (subpackage of the already-
direct `bubbles v1.0.0` dependency) exists precisely for this: bindings
carry their own help text, and help renders from the same definitions the
handler matches on.

## Current state

All excerpts below are from commit `76befde` (post-plans 009/013/014). Line
numbers match the live file; `output.Describe` exists from plan 014.

- `internal/tui/tui.go:25-31` — modes:

  ```go
  type mode int
  const (
      modeList mode = iota
      modeConfirm
      modeDetail
      modeFilter
  )
  ```

- `internal/tui/tui.go:44-73` — `Model` struct (relevant fields):

  ```go
  type Model struct {
      cfg    config.Config
      raw    []pm.Server
      rows   []pm.Server
      table  table.Model
      ti     textinput.Model
      mode        mode
      all         bool
      query       string
      selected    pm.Server
      planTree    kill.Plan
      planSingle  kill.Plan
      killSingle  bool
      theme       Theme
      status      string
      err         error
      width, height int
      loadSeq, appliedSeq int
      loading     bool
      previewBoth func(pm.Server, kill.Opts) (kill.Plan, kill.Plan)
  }
  ```

- `internal/tui/tui.go:254-346` — `handleKey` (full current implementation):

  ```go
  func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
      if msg.String() == "ctrl+c" {
          return m, tea.Quit
      }
      switch m.mode {
      case modeFilter:
          switch msg.String() {
          case "esc":
              m.query = ""
              m.ti.SetValue("")
              m.ti.Blur()
              m.mode = modeList
              m.rebuild()
              return m, nil
          case "enter":
              m.ti.Blur()
              m.mode = modeList
              return m, nil
          }
          var cmd tea.Cmd
          m.ti, cmd = m.ti.Update(msg)
          m.query = m.ti.Value()
          m.rebuild()
          return m, cmd
      case modeConfirm:
          switch strings.ToLower(msg.String()) {
          case "y", "yes":
              opts := m.killOpts()
              m.mode = modeList
              m.status = "killing " + output.Describe(m.selected) + "…"
              return m, killCmd(m.selected, opts)
          case "s":
              plan := m.currentPlan()
              if !plan.Docker && !plan.NoPID {
                  m.killSingle = !m.killSingle
              }
              return m, nil
          default:
              m.mode = modeList
              return m, nil
          }
      case modeDetail:
          switch msg.String() {
          case "esc", "enter", "q":
              m.mode = modeList
          }
          return m, nil
      }
      // modeList
      switch msg.String() {
      case "q", "esc":
          return m, tea.Quit
      case "r":
          m.status = ""
          nm, loadC := m.nextLoadCmd()
          return nm, loadC
      case "a":
          m.all = !m.all
          m.rebuild()
          return m, nil
      case "t":
          m = m.cycleTheme()
          return m, persistThemeCmd(m.cfg)
      case "/":
          m.mode = modeFilter
          m.ti.Focus()
          return m, textinput.Blink
      case "x":
          if s, ok := m.current(); ok {
              m.selected = s
              m.killSingle = false
              m.planTree, m.planSingle = m.previewBoth(s, m.killOpts())
              m.mode = modeConfirm
          }
          return m, nil
      case "enter":
          if s, ok := m.current(); ok {
              m.selected = s
              m.mode = modeDetail
          }
          return m, nil
      }
      var cmd tea.Cmd
      m.table, cmd = m.table.Update(msg)
      return m, cmd
  }
  ```

- `internal/tui/tui.go:438-444` — `footerView` (hand-maintained string):

  ```go
  func (m Model) footerView() string {
      help := dimStyle.Render("↑/↓ move · x kill · enter details · / filter · a all · t theme · r refresh · q/esc quit")
      if m.status != "" {
          return m.status + "\n" + help
      }
      return help
  }
  ```

- `internal/tui/tui.go:483` — confirm-mode inline help (inside `confirmView`):

  ```go
  b.WriteString(dimStyle.Render("scope: "+scope+" · s to toggle") + "\n")
  ```

- `internal/tui/tui.go:520` — detail-mode inline help (inside `detailView`):

  ```go
  b.WriteString("\n" + dimStyle.Render("esc back · q list"))
  ```

- README TUI key table (`README.md:86-97`): documents list-mode keys only.
- `internal/tui/tui_test.go` — 23 test functions. Key-handling tests are all
  white-box, driving keys via `key("x")`, `key("y")`, `key("esc")`, etc.
  (helper defined at line 50 of the test file). They pin behavior per key.
- `bubbles v1.0.0` is a direct dependency (`go.mod:7`); `bubbles/key`
  imports `key.NewBinding`, `key.WithKeys`, `key.WithHelp`, `key.Matches` —
  all confirmed available at the pinned version (no go.mod changes needed).
  Import path: `"github.com/charmbracelet/bubbles/key"`.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| TUI tests | `go test ./internal/tui/ -v -count=1` | pass |
| Suite | `make lint && make test` | exit 0 |
| Manual smoke | `make run ARGS=tui` | keys work; footer changes per mode |

## Scope

**In scope**:
- New `internal/tui/keys.go` (keymap definition — keeps `tui.go` shrinking)
- `internal/tui/tui.go` (add `keys` field to `Model`; rewrite `handleKey` to
  use `key.Matches`; replace `footerView` with generated help; delete inline
  help from `confirmView` and `detailView`)
- `internal/tui/tui_test.go` (additions only — per-mode footer help tests)
- `README.md` TUI key table (add new confirm/detail rows IF any binding doc
  changes)

**Out of scope**:
- Changing ANY key binding or adding new keys (plan 019 adds sort; lands
  after this).
- The bubbles `help` widget — the footer stays a one-line builder.
- Splitting view functions into `view.go` (deferred to plan 022).

## Git workflow

- Branch: `advisor/017-tui-keymap`
- Commit per step.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Define the keymap and add it to the Model

New `internal/tui/keys.go`:

```go
package tui

import "github.com/charmbracelet/bubbles/key"

type keyMap struct {
    // List-mode bindings
    Up, Down              key.Binding
    Kill, Detail, Filter  key.Binding
    All, Theme, Refresh   key.Binding
    Quit                  key.Binding

    // Confirm-mode bindings
    ConfirmYes, ConfirmScope key.Binding
    ConfirmCancel            key.Binding

    // Detail-mode bindings
    DetailBack key.Binding

    // Filter-mode bindings
    FilterApply, FilterCancel key.Binding
}

func defaultKeyMap() keyMap {
    return keyMap{
        Up:      key.NewBinding(key.WithKeys("up"), key.WithHelp("↑/↓", "move")),
        Down:    key.NewBinding(key.WithKeys("down"), key.WithHelp("↑/↓", "move")),
        Kill:    key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "kill")),
        Detail:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "details")),
        Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
        All:     key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "all")),
        Theme:   key.NewBinding(key.WithKeys("t"), key.WithHelp("t", "theme")),
        Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
        Quit:    key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q/esc", "quit")),

        ConfirmYes:    key.NewBinding(key.WithKeys("y", "yes"), key.WithHelp("y", "confirm")),
        ConfirmScope:  key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "toggle scope")),
        ConfirmCancel: key.NewBinding(key.WithKeys("n", "esc"), key.WithHelp("n/esc", "cancel")),

        DetailBack: key.NewBinding(key.WithKeys("esc", "enter", "q"), key.WithHelp("esc/enter/q", "back")),

        FilterApply:  key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
        FilterCancel: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
    }
}
```

Note: `ConfirmYes` includes `"yes"` as a key string because `handleKey`
currently matches `"yes"` in confirm mode. `key.Matches` does exact string
comparison against `Key.String()`, so the binding must list every variant.
The lowercase `"y"` and `"yes"` in the binding work because `handleKey`
calls `strings.ToLower(msg.String())` before the confirm-mode switch (keep
it — it also handles accidental uppercase).

Add `keys keyMap` to the `Model` struct in `tui.go` and set `keys:
defaultKeyMap()` in `New()`.

**Verify**: `go build ./...` → exit 0 (no unused imports, clean compile).

### Step 2: Rewrite `handleKey` to use `key.Matches`

Replace the string-literal comparisons in `handleKey` with `key.Matches(msg,
m.keys.X)`. Import `key` in `tui.go`.

The `ctrl+c` handling at the top stays a string literal (bubbletea
convention; `ctrl+c` is a special key string).

Preserve the exact structure and order — mode switch outer, per-mode inner.
The confirm-mode `default` → modeList gate is preserved (with `ConfirmCancel`
only providing the help text — it doesn't gate behavior).

```go
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
    if msg.String() == "ctrl+c" {
        return m, tea.Quit
    }

    switch m.mode {
    case modeFilter:
        if key.Matches(msg, m.keys.FilterCancel) {
            m.query = ""
            m.ti.SetValue("")
            m.ti.Blur()
            m.mode = modeList
            m.rebuild()
            return m, nil
        }
        if key.Matches(msg, m.keys.FilterApply) {
            m.ti.Blur()
            m.mode = modeList
            return m, nil
        }
        var cmd tea.Cmd
        m.ti, cmd = m.ti.Update(msg)
        m.query = m.ti.Value()
        m.rebuild()
        return m, cmd

    case modeConfirm:
        // keep strings.ToLower so Y and YES also work without doubling
        // every key variant in the binding.
        switch strings.ToLower(msg.String()) {
        case "y", "yes":
            opts := m.killOpts()
            m.mode = modeList
            m.status = "killing " + output.Describe(m.selected) + "…"
            return m, killCmd(m.selected, opts)
        case "s":
            plan := m.currentPlan()
            if !plan.Docker && !plan.NoPID {
                m.killSingle = !m.killSingle
            }
            return m, nil
        default:
            m.mode = modeList
            return m, nil
        }

    case modeDetail:
        if key.Matches(msg, m.keys.DetailBack) {
            m.mode = modeList
        }
        return m, nil
    }

    // modeList
    switch {
    case key.Matches(msg, m.keys.Quit):
        return m, tea.Quit
    case key.Matches(msg, m.keys.Refresh):
        m.status = ""
        nm, loadC := m.nextLoadCmd()
        return nm, loadC
    case key.Matches(msg, m.keys.All):
        m.all = !m.all
        m.rebuild()
        return m, nil
    case key.Matches(msg, m.keys.Theme):
        m = m.cycleTheme()
        return m, persistThemeCmd(m.cfg)
    case key.Matches(msg, m.keys.Filter):
        m.mode = modeFilter
        m.ti.Focus()
        return m, textinput.Blink
    case key.Matches(msg, m.keys.Kill):
        if s, ok := m.current(); ok {
            m.selected = s
            m.killSingle = false
            m.planTree, m.planSingle = m.previewBoth(s, m.killOpts())
            m.mode = modeConfirm
        }
        return m, nil
    case key.Matches(msg, m.keys.Detail):
        if s, ok := m.current(); ok {
            m.selected = s
            m.mode = modeDetail
        }
        return m, nil
    }

    var cmd tea.Cmd
    m.table, cmd = m.table.Update(msg)
    return m, cmd
}
```

Note: `key.Matches` is variadic, accepting multiple bindings. The
modeList switch uses a `switch { case key.Matches(...): ... }` idiom
instead of nested `if`. This is a Go-idiomatic way to chain matches.

**Verify**: `go test ./internal/tui/ -v -count=1` → ALL 23 pre-existing
tests pass unchanged. This is the critical gate — any test failure means a
binding was dropped or changed. If a test fails, STOP and fix the
transcription; do NOT modify test expectations.

### Step 3: Generate footer help from the keymap

Add a `helpLine()` method to `Model` that builds the help string from the
current mode's bindings:

```go
// helpLine returns a dimmed " · "-separated help string for the current
// mode, built from the same key.Binding definitions the handler matches on.
func (m Model) helpLine() string {
    var parts []string
    add := func(b key.Binding) {
        parts = append(parts, b.Help().Key+" "+b.Help().Desc)
    }

    switch m.mode {
    case modeList:
        add(m.keys.Up)
        add(m.keys.Kill)
        add(m.keys.Detail)
        add(m.keys.Filter)
        add(m.keys.All)
        add(m.keys.Theme)
        add(m.keys.Refresh)
        add(m.keys.Quit)
    case modeConfirm:
        add(m.keys.ConfirmYes)
        // Scope toggle is only meaningful for native process trees.
        plan := m.currentPlan()
        if !plan.Docker && !plan.NoPID {
            add(m.keys.ConfirmScope)
        }
        add(m.keys.ConfirmCancel)
    case modeDetail:
        add(m.keys.DetailBack)
    case modeFilter:
        add(m.keys.FilterApply)
        add(m.keys.FilterCancel)
    }

    return dimStyle.Render(strings.Join(parts, " · "))
}
```

Replace `footerView`:

```go
func (m Model) footerView() string {
    help := m.helpLine()
    if m.status != "" {
        return m.status + "\n" + help
    }
    return help
}
```

Delete the inline help lines:
- `confirmView`: remove the `scope: … · s to toggle` line (line 483).
- `detailView`: remove the `esc back · q list` line (line 520).

The scope toggle state is still visible in the confirm header — the `head`
line already says `" — %d processes"` with the count. The inline toggle hint
is redundant now that the footer shows `s toggle scope`. The detail back
keys are now in the footer.

Add tests to `tui_test.go`:

```go
func TestFooterHelp_ListMode(t *testing.T) {
    m := newLoaded()
    v := m.View()
    for _, want := range []string{"move", "kill", "details", "filter", "all", "theme", "refresh", "quit"} {
        if !strings.Contains(v, want) {
            t.Errorf("list-mode footer missing %q", want)
        }
    }
}

func TestFooterHelp_ConfirmMode(t *testing.T) {
    m := newLoaded()
    m.previewBoth = func(s pm.Server, _ kill.Opts) (kill.Plan, kill.Plan) {
        return kill.Plan{Tree: []kill.TreeMember{{PID: 100, Name: "node"}}},
            kill.Plan{Tree: []kill.TreeMember{{PID: 100, Name: "node"}}}
    }
    m = step(m, key("x"))
    v := m.View()
    if !strings.Contains(v, "confirm") {
        t.Error("confirm footer missing 'confirm'")
    }
    if !strings.Contains(v, "toggle scope") {
        t.Error("confirm footer missing 'toggle scope'")
    }
    if !strings.Contains(v, "cancel") {
        t.Error("confirm footer missing 'cancel'")
    }
}

func TestFooterHelp_ConfirmScopeAbsentForDocker(t *testing.T) {
    m := newLoadedDocker()
    m = step(m, key("x"))
    v := m.View()
    if strings.Contains(v, "toggle scope") {
        t.Error("docker confirm must not show toggle scope")
    }
}

func TestFooterHelp_DetailMode(t *testing.T) {
    m := newLoaded()
    m = step(m, key("enter"))
    v := m.View()
    if !strings.Contains(v, "back") {
        t.Error("detail footer missing 'back'")
    }
}

func TestFooterHelp_FilterMode(t *testing.T) {
    m := newLoaded()
    m = step(m, key("/"))
    v := m.View()
    if !strings.Contains(v, "apply") {
        t.Error("filter footer missing 'apply'")
    }
    if !strings.Contains(v, "clear") {
        t.Error("filter footer missing 'clear'")
    }
}
```

**Verify**: `go test ./internal/tui/ -v -count=1` → all 28 tests pass
(23 existing + 5 new). Update only tests that pinned the deleted inline
hint strings — `TestConfirmPreviewsBlastRadius` checks `View()` for `"100"`
which still appears in the confirm tree rendering, so it should pass
unchanged. List any test expectation updates in the final report.

### Step 4: Gate

**Verify**: `make lint && make test` → exit 0. Cross-check `README.md`'s
TUI key table — the list-mode keys are unchanged; no README edits needed.

## Test plan

- `tui_test.go`: 5 new help-rendering tests (Step 3) — verify the footer
  contains expected terms per mode.
- All 23 pre-existing key-driving tests must pass without modification after
  Step 2 (binding transcription gate).
- Pattern: existing Update-pumping tests. The new tests follow the same
  pattern as `TestConfirmPreviewsBlastRadius` (build model, pump keys,
  check `m.View()` substrings).

## Done criteria

- [ ] `internal/tui/keys.go` exists with `keyMap` and `defaultKeyMap()`
- [ ] `handleKey` uses `key.Matches` — no raw `switch msg.String()` for
      key literals remains (except `ctrl+c` at the top)
- [ ] `footerView` calls `helpLine()`; inline help deleted from
      `confirmView` and `detailView`
- [ ] `grep -n "dimStyle.Render.*esc back" internal/tui/tui.go` → no match
- [ ] All 23 pre-existing key tests pass without modification
- [ ] 5 new footer-help tests pass
- [ ] `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- Any pre-existing key test requires a behavior-level change to pass after
  Step 2 — a binding got dropped or mis-transcribed. Fix the keymap
  definition, and if it still fails, stop and report the test name and
  failure message.
- `bubbles/key` at v1.0.0 lacks any of `NewBinding`, `WithKeys`, `WithHelp`,
  or `Matches`. Verify: `go doc github.com/charmbracelet/bubbles/key
  NewBinding` → function signature exists. (It does — confirmed at
  v1.0.0; this is just the safety check.)
- The confirm-mode `s` (scope toggle) binding fails because `handleKey`
  uses `strings.ToLower` and `key.Matches` does not — the `s` binding is
  NOT affected by this because the confirm-mode `s` case still uses a
  string switch (not `key.Matches`). See the code in Step 2.
- Any test assertion fails that is NOT listed in this plan — the code has
  drifted since `76befde`; report the failing test and line.

## Maintenance notes

- Plan 019 adds a sort key as one `key.Binding` + one help entry — the
  payoff of this plan. New keys now get added to `defaultKeyMap()`,
  `handleKey`, and `helpLine()`'s modeLists section; reviewers should
  reject future raw-literal keys.
- Plan 022 (bubbletea v2) changes `tea.KeyMsg`; `bubbles/v2/key` has the
  matching API. The `keyMap` struct survives the migration — only the import
  paths change.
- Deferred: splitting view functions into `view.go` — do it opportunistically
  in plan 022's migration if churn is already total.
