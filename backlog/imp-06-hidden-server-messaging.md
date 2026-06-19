# imp-06 — Tell the user when servers are hidden

**Status:** todo
**Priority:** medium — clarity; avoids a confusing "nothing here" dead end
**Category:** UX
**Effort:** ~30 min

## Problem

When nothing clears the confidence threshold, `output.Table`
(`internal/output/output.go:24`) prints:

```
No listening servers found.
```

But that's often false. The machine may have ten listening ports — they just
scored below `confidence_threshold`, or sit on root-owned sockets with no PID.
The user is told "nothing's running" when the truth is "nothing matched your
filters." There's no hint that `--all` exists or that N ports were suppressed.

The same dead end appears in the TUI header (`tui.go:332`), which shows
`0 shown · yours` with no nudge toward `a` (toggle all).

## Why it matters

This is the most likely first-run experience on a fresh machine or one with only
system services: the tool looks broken or empty. A one-line hint converts a
confusing dead end into an obvious next step, and teaches the `--all` / `a`
affordance exactly when it's relevant.

## Suggested approach

`inventory.View` already has both the full inventory and the filtered result, so
the count of hidden rows is computable without a second scan. Thread a small
summary to the renderers (or compute it in the CLI/TUI before rendering):

**CLI list** — when the filtered table is empty but the raw inventory wasn't:

```
No servers matched (3 listening ports hidden below the confidence threshold).
Run `whence list --all` to see everything, or lower confidence_threshold.
```

Distinguish the causes when you can:
- hidden by threshold → suggest `--all` / lowering the threshold;
- present but unattributed (no PID, root-owned) → suggest rerunning elevated
  (the `doctor` note at `cli/doctor.go:88` already has this wording to reuse).

**TUI** — when `len(m.rows) == 0` and `len(m.raw) > 0`, render a centered hint in
the table area: `0 of N shown — press a to show all`.

## Tests / verification

- `output` golden/unit test: empty filtered list + non-empty raw → the hint
  string (not the bare "No listening servers found.").
- TUI test (`tui_test.go` style): load servers all below threshold, assert
  `View()` contains the "press a" hint.

## Notes / trade-offs

- Keep the hint on the **table** path only; `--json` must stay pure data (an
  empty array), never prose.
- Don't over-explain. One line with the actionable flag is enough; the full
  story lives in `whence doctor`.
