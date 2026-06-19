# imp-09 — Polish grab-bag

**Status:** todo
**Priority:** low — small, independent quality-of-life fixes
**Category:** UX / polish
**Effort:** ~15 min each

A collection of minor improvements, each cheap and standalone. Cherry-pick.

## 1. Show the version in `doctor`

`doctor` (`cli/doctor.go`) reports platform, Go version, config, lsof, docker,
and socket stats — but not `whence`'s own version (`cli/root.go:12`, injected via
ldflags). For a tool people will install via brew/scoop and file issues about,
the build version is the single most useful line in a diagnostics command. Add:

```go
row("whence version", version)
```

(Move the `version` var or expose a getter so `doctor.go` can read it.)

## 2. Human-readable uptime in JSON

`--json` serializes `Uptime` as `uptimeNs` — raw nanoseconds
(`model.go:29`, a `time.Duration`). Machine-parseable but awkward; every consumer
has to divide by 1e9. Consider adding a sibling string field (`"uptime": "3h17m"`
via `output.HumanUptime`) or an ISO-8601-ish `"uptimeSeconds"`, while keeping the
ns field for compatibility. Decide deliberately — the JSON shape is a public
contract, so add rather than rename, and pin it with a golden test (see imp-05).

## 3. `--watch` redraw flicker

`watchList` (`cli/list.go:82`) clears the whole screen each interval with
`\033[H\033[2J` then reprints. On slower terminals this flickers, and a
`time.Sleep` loop ignores `SIGINT` cleanliness (the screen-clear escape can
linger). Options:
- redraw without a full clear (move cursor home, overwrite, clear-to-end per
  line), or
- just point users at `whence tui`, which already does live refresh properly,
  and consider whether `--watch` earns its keep alongside it.

## 4. `--single` + project-kill interaction

`whence kill <name> --single` applies `Single` to every matched unit. For a
multi-process project that's an odd combination (kill only each listener, leave
its tree). Not wrong, but worth a one-line doc note on `--single`, or a guard
that warns when `--single` is combined with a name (multi-unit) target.

## 5. Drop or document dead-ish fields

If **imp-07** (surface `Address`) isn't taken, `model.Server.Address` is
collected and only ever appears in JSON — decide whether to surface or remove it.
Avoid carrying struct fields no human view reads.

## Notes

These are intentionally last: none affects correctness or safety. Knock them out
opportunistically when touching the relevant file for a higher-priority item
(e.g. do #1 while adding the docker-timeout report from imp-01; do #2 while
writing the output golden tests from imp-05).
