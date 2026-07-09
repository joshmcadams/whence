# imp-08 — Eliminate redundant work in the scan path

**Status:** done (see commit 49802a5)
**Priority:** medium-low — perf; noticeable mainly with many ports / on macOS
**Category:** performance
**Effort:** ~1–2 hr (independent sub-items)

## Problem

Several hot paths redo work they could share. None is catastrophic, but together
they make a busy machine's scan slower than it needs to be — and the TUI re-runs
the whole thing every 5s (`refreshInterval`, `tui.go:23`).

1. **Project detection re-reads disk per server.** `classify.Process`
   (`classify.go:45`) calls `project.Detect(s.Cwd)` for every native server, and
   `Detect` walks parents and reads `package.json`/`go.mod`/README from disk each
   time (`project.go:28`). Two servers in the same repo (a common front-end +
   back-end pair, or one process holding several ports) pay the full walk twice.
   The Docker path independently calls `project.Description(workdir)`
   (`docker.go:105`) per container, re-reading the same README.

2. **The kill preview snapshots the whole process table per unit.**
   `confirmKill` (`cli/kill.go:137`) loops over units calling `kill.Preview`,
   and each `Preview` calls `snapshot()` (`kill.go:64`), which enumerates **all**
   processes via gopsutil. Killing a 3-port project snapshots the process table
   three times.

3. **Native scan and Docker run sequentially.** `inventory.Collect`
   (`inventory.go:20`) does `scan.Processes()` then `docker.Servers()` back to
   back, even though DESIGN.md calls Docker a "parallel detection path." They're
   independent and could overlap.

4. **macOS spawns one `lsof` per PID.** `cwd_darwin.go:20` runs `lsof` once per
   process; N listening sockets means N process spawns, each ~10–50ms.

## Why it matters

The TUI's auto-refresh makes the scan a recurring cost, not a one-shot. On a
developer box with a dozen services and a Docker stack, the redundant disk reads
and serial Docker call add up to visible lag on every tick — and macOS users pay
the per-PID `lsof` tax on top.

## Suggested approach

Independent, do any subset:

1. **Memoize project resolution by root.** Cache `Detect`/`Description` results
   keyed by the resolved root dir for the duration of one `Collect`. A
   `map[string]*model.Project` populated as you go turns repeated repos into a
   hit. Description reads especially benefit (README parsing is the priciest bit).

2. **Snapshot once for a multi-target preview.** Take one `snapshot()` in
   `confirmKill` and pass the table into `Preview`/`planTree` (the plumbing
   already accepts a `procTable`; just hoist the call). Same fix for the actual
   multi-kill loop.

3. **Overlap scan + docker.** Run `docker.Servers()` in a goroutine while
   `scan.Processes()` runs, then merge. This also hides the Docker timeout budget
   from **imp-01** behind work you're already doing.

4. **Batch macOS `lsof`.** `lsof -a -d cwd -Fpn -p <pid1>,<pid2>,…` returns cwds
   for many PIDs in one spawn; parse the `p<pid>`/`n<path>` field stream into a
   `map[pid]cwd`. One process instead of N.

## Tests / verification

- Project cache: add a counter/seam in a test to assert `Detect` reads each root
  once across a multi-server `Collect`.
- Preview: assert `snapshot()` is invoked once for a multi-unit confirm (inject a
  fake/counting table source).
- Bench (optional): `go test -bench` over `Collect` with a synthetic 20-server
  inventory before/after.

## Notes / trade-offs

- The cache is per-invocation only — `whence` keeps no daemon/persistence
  (a stated design value), so don't introduce a cross-run cache.
- Parallelizing adds a little concurrency complexity; keep the merge
  deterministic (sort already happens in `View`) so output stays stable.
- Measure before investing in #4 — it only matters on macOS and only with many
  ports.
