# imp-05 — Test the untested core packages

**Status:** done (merged to main)
**Priority:** medium-high — core logic with subtle edges is currently unguarded
**Category:** testing
**Effort:** ~2 hr

> **Implemented.** Coverage of the four targets went from 0% to: `config` 75%,
> `output` 88%, `model` 100%, `scan` 7% (only `protoOf` is unit-testable; the
> rest is live-system cwd/socket code, as anticipated — not worth mocking).
> - `config_test.go`: `IsUnderDevRoot` table including the `~/dev` vs `~/devil`
>   boundary, ancestor/empty/unrelated cases, and the case-insensitive match
>   (encoding the backlog-05 trade-off so it can't be "fixed" by accident);
>   `Load` missing-file→defaults and partial-file overlay; `Save`/`Load`
>   round-trip; `Path` shape (all via a temp `XDG_CONFIG_HOME`).
> - `output_test.go`: `HumanUptime` boundaries (59s/60s/59m/1h/1d…), `Truncate`
>   (exact length, ellipsis, `n<=1`, multi-byte runes), `SrcLabel`, `Table`
>   empty + row, and a `JSON` field-name contract test (locks `uptimeNs` etc.).
> - `model_test.go`: `DisplayName` / `Description` / `Attributed`.
> - `scan_test.go`: `protoOf` family→tcp/tcp6 mapping.

## Problem

`go test -cover ./...` today:

```
internal/config      0.0%      ← the "is this mine?" gate lives here
internal/output      0.0%      ← every table/JSON byte the user sees
internal/model       0.0%
internal/scan        0.0%
internal/cli         12.4%
internal/kill        21.5%
```

The well-tested packages (`project` 79%, `classify` 65%, `docker`, `inventory`,
`tui`) prove the team knows how to test this code — these gaps are just unfilled.
Two of them cover logic that is both **central** and **edge-case-prone**.

### `config.IsUnderDevRoot` — the heart of classification, untested

`internal/config/config.go:111` decides whether a cwd counts as "yours," which
drives the +50 dev-root score and therefore what `whence` shows by default. Its
prefix logic has a deliberate subtlety:

```go
if d == r || strings.HasPrefix(d, r+string(os.PathSeparator)) { ... }
```

The `r + separator` guard is what stops `~/devil` from matching dev-root
`~/dev` — exactly the kind of off-by-one that a careless "simplification" could
reintroduce. There is no test pinning it. (See also backlog item 05 on the
case-insensitive-on-Linux trade-off — a test here is where that decision should
be encoded.)

### `output` formatters — pure, user-facing, trivial to test

`HumanUptime` (`output.go:79`) and `Truncate` (`output.go:67`) are pure
functions with clear boundaries that nothing currently exercises.

## Why it matters

These are the functions most likely to be "tidied" by a future change (including
an AI agent's), and the ones whose silent breakage is most visible to users
(wrong attribution, mangled table). Cheap, fast, white-box unit tests here buy a
lot of regression safety per line.

## Suggested approach

Add white-box tests (`package foo`, same dir) in the established house style:

**`config`**
- `IsUnderDevRoot`: exact root match; child path; the `~/dev` vs `~/devil`
  non-match; empty input → false; a path above the root → false. Decide and
  encode the case sensitivity behavior (ties into backlog 05).
- `Load`: missing file returns `Default()` with no error; a partial TOML file
  overlays onto defaults (set `XDG_CONFIG_HOME` to a temp dir).
- `Save` → `Load` round-trips a config; `Path` honors `XDG_CONFIG_HOME` /
  `AppData`.

**`output`**
- `HumanUptime`: `0/negative → "-"`, `45s`, `12m`, `3h17m`, `2d4h`, and the
  exact minute/hour boundaries (59s, 60s, 3599s, 3600s).
- `Truncate`: shorter-than-limit unchanged; exact length; cut adds `…`; the
  `n <= 1` branch; multi-byte runes counted correctly (an emoji/CJK string).
- `Table`/`JSON`: a small golden test over a fixed `[]Server` to lock the
  column layout and the JSON field names (the JSON shape is a public contract —
  see imp-09 about `uptimeNs`).

**`model`** (quick wins): `DisplayName` prefers project name then falls back to
`Name`; `Description` nil-safe; `Attributed` is `PID > 0`.

**`scan`**: `protoOf` (`scan.go:99`) maps families → `tcp`/`tcp6` and is pure —
test it directly. (The cwd resolvers are OS- and privilege-bound and not worth
faking; leave those to `doctor` + manual checks.)

## Tests / verification

`make test` stays green; `go test -cover ./...` shows `config` and `output`
climbing from 0 toward parity with `project`. Consider adding a coverage
threshold gate to CI once the core packages are covered.

## Notes / trade-offs

- Don't chase 100%: the `cli` and `scan` numbers are low mostly because they
  touch the live system, which isn't worth mocking wholesale. Target the **pure,
  high-leverage** logic (config matcher, formatters) rather than coverage for its
  own sake.
