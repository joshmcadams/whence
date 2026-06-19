# imp-04 — Make `kill <name>` prefer exact matches

**Status:** todo
**Priority:** medium-high — destructive over-match
**Category:** safety / UX
**Effort:** ~30 min

## Problem

`matchTargets` (`internal/cli/kill.go:92`) resolves a non-numeric target with
**substring** matching against three fields:

```go
want := strings.ToLower(target)
return filter(servers, func(s model.Server) bool {
    if strings.Contains(strings.ToLower(s.DisplayName()), want) { return true }
    if strings.Contains(strings.ToLower(s.Name), want) { return true }
    return s.Project != nil && strings.Contains(strings.ToLower(s.Project.Name), want)
})
```

So `whence kill api` matches `api`, `api-gateway`, `payments-api`, and
`api-docs` all at once. For a **destructive** command, substring matching is a
foot-gun: the obvious-looking `whence kill app` can sweep up unrelated projects.

The confirmation prompt does enumerate everything (good — and `--force` bypasses
it), but the *default selection* being this broad is surprising and asymmetric
with how people think about names.

## Why it matters

Kill is the one irreversible action in the tool. The matcher should bias toward
"do exactly what I named," not "everything that contains these letters." Most of
the safety machinery (tree preview, confirm prompt) is about not killing too
much; the target selector quietly works against that.

## Suggested approach

Tiered matching — exact first, substring only as a fallback:

1. Collect **exact** (case-insensitive) matches on `DisplayName` / `Name` /
   `Project.Name`. If any exist, use only those.
2. Otherwise fall back to the current substring behavior, but surface it:
   when the match was fuzzy, the confirmation header should say so — e.g.
   `No exact match for "app"; 3 servers contain it:` — so the user understands
   why four things are about to die.

```go
if exact := matchExact(servers, want); len(exact) > 0 {
    return exact
}
return matchSubstring(servers, want) // today's behavior, flagged downstream
```

This keeps the convenience of partial names (handy in the TUI-less CLI) while
making the precise case precise.

## Tests / verification

`cli/kill_test.go` already tests `matchTargets`. Add cases:
- two servers `api` and `api-gateway`; `kill api` selects only `api`.
- target `gate` (no exact match) still finds `api-gateway` via fallback.
- numeric targets remain port-only (existing
  `TestMatchTargets_NumericIsAlwaysPort` must stay green).

## Notes / trade-offs

- Exact-first can mildly surprise someone who *relied* on substring (e.g. typed
  a prefix expecting the fuzzy hit). The flagged-fallback message mitigates this,
  and the confirmation still lists everything before acting.
- Consider applying the same exact-first tiering to the TUI `/` filter? No —
  filtering is non-destructive and read-only there; broad substring is the right
  default for search. This change is specifically for the *kill selector*.
