# imp-07 — Surface bind address / network exposure

**Status:** done (see commit 0a591c2)
**Priority:** medium — useful signal from data already collected; mild security value
**Category:** UX / feature
**Effort:** ~45 min

## Problem

`scan.Processes` already captures the listen address —
`Address: c.Laddr.IP` (`internal/scan/scan.go:44`) — and `model.Server` carries
it (`Address string json:"address"`, `model.go:21`). But `grep -rn "\.Address"`
shows it's surfaced **nowhere**: not in the table (`output.go:29`), not in the
TUI table or detail view (`tui.go:356`). It rides along only in `--json`.

So the tool collects whether a dev server is bound to `127.0.0.1` (local only)
or `0.0.0.0` / `::` (reachable by anything on your LAN/VPN) — and then throws
that away in every human view.

## Why it matters

For a "what am I running and where did it come from" tool, *who can reach it* is
a natural and genuinely useful third question — and a mild security signal. A
dev database or an unauthenticated admin server inadvertently bound to `0.0.0.0`
is a real, common mistake (Vite, many Node servers, and `docker run -p` default
to all-interfaces). Showing it costs almost nothing because the data is already
in hand.

## Suggested approach

Add a derived helper on `model.Server` and render it:

```go
// Exposure classifies the bind address for display.
func (s Server) Exposure() string {
    switch s.Address {
    case "127.0.0.1", "::1", "localhost":
        return "local"
    case "", "0.0.0.0", "::":
        return "all"   // reachable off-box
    default:
        return s.Address // a specific interface IP
    }
}
```

- **Detail view** (`tui.go:detailView`): always show `Address` / `Exposure` —
  the detail pane is the right place for full per-row facts and has room.
- **Tables / TUI list**: optionally tag only the noteworthy case, e.g. an
  `EXPOSED` marker (or a colored dot) on rows bound to all interfaces, rather
  than a whole new always-on column. Keeps the common `local` case quiet.
- **Docker**: the container path can fill `Address` from the published
  `HostIP` (`docker.go` already reads `HostIp` in `hostPorts` but discards it) —
  a `0.0.0.0` host binding is the same "exposed" signal.

## Tests / verification

- Unit-test `Exposure()` across `127.0.0.1`, `::1`, `0.0.0.0`, `::`, empty, and
  a specific IP.
- TUI detail-view test asserts the address line renders.
- Capture `HostIP` in `docker.hostPorts` and assert it propagates to
  `Server.Address`.

## Notes / trade-offs

- This is the only field the model collects but never shows a human; either
  surface it (this item) or drop it from the struct. Surfacing is the better
  call given the value.
- Don't overstate it as a security feature — binding to `0.0.0.0` on a laptop
  behind a firewall is usually harmless. Frame it as visibility ("reachable off
  this machine"), not a warning.
