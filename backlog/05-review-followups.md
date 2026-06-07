# 05 — Review follow-ups (deferred design calls)

**Status:** todo
**Priority:** low — neither is a correctness bug; both are judgement calls
**Owner:** you

Two findings from the 2026-06-07 critical review. They were reported and left
unfixed on purpose: each is a deliberate trade-off, not a defect, so the call is
yours. (The third finding from that review — the kill confirmation not showing
the full process tree — was fixed.)

## 1. A process bound to both IPv4 and IPv6 shows as two rows

`scan.Processes` dedups listening sockets on `port/proto/pid`, and `protoOf`
maps the address family to `tcp` vs `tcp6`. A server that binds both stacks
(`0.0.0.0:3000` **and** `:::3000` — common for Vite, many Node servers) therefore
produces two rows with the same PID, project, and uptime.

- **Keep as-is** if per-stack visibility is wanted (you can see it's dual-bound).
- **Collapse** by deduping on `(port, pid)` and showing the proto as `tcp` (or
  `tcp/tcp6`) when both are present. Touch point: the dedup key in
  `internal/scan/scan.go:35`; the merge in `internal/inventory/Collect` would
  also need to stay consistent.

Decide intent, then either collapse or add a one-line code comment marking it as
intentional so it doesn't get "fixed" later by mistake.

## 2. Dev-root matching is case-insensitive on Linux too

`config.IsUnderDevRoot` lowercases both sides (`normalize` in
`internal/config/config.go:125`) so that `~/Development` and `~/development`, and
case-insensitive filesystems (macOS/Windows), all match. The side effect: on
Linux (case-sensitive FS) a path like `~/Dev/x` would match a configured
`~/dev` root even though they're genuinely different directories.

- Very low real-world risk (you'd need two dev roots differing only in case).
- **Option:** make the comparison case-sensitive on `runtime.GOOS == "linux"`
  only, or normalize case only on darwin/windows. Adds a platform branch to an
  otherwise simple function — weigh against the negligible risk.

Likely fine to leave with a clarifying comment; filed so the trade-off is on the
record.
