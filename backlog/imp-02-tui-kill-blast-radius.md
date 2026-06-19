# imp-02 — Show the full kill blast radius in the TUI confirmation

**Status:** todo
**Priority:** high — safety; the TUI can kill more than it shows
**Category:** UX / safety
**Effort:** ~1 hr

## Problem

The CLI kill path goes to real lengths so the confirmation can't understate what
will die. `cli/kill.go:confirmKill` calls `kill.Preview` for each target and
prints the entire climbed tree (root + descendants, with a process count), and
`internal/kill/AGENTS.md` enshrines this:

> So that the confirmation can't lie about the blast radius, the scope is
> computed in exactly one place — `planTree` — which both `killProcess` and
> `Preview` call.

The **TUI does not honor this.** Pressing `x` sets `modeConfirm`
(`tui.go:253`), and the confirm view renders a single line
(`tui.go:323`):

```
Kill :3000 myapp (pid 12345) ?  [y/N]
```

`kill.Preview` is never called in the TUI (`grep "Preview" internal/tui` → no
hits). So when the listening process climbs to a shared launcher — one
`make dev` / `npm` script that starts several services — the TUI user confirms a
single PID but actually terminates the whole subtree, including sibling
services, with no warning. That's precisely the "shared launcher" sharp edge the
CLI was hardened against in the root `AGENTS.md` caveats.

## Why it matters

The TUI is the friendlier, more-clicked surface — and the one where a stray `x`
is easiest. It should be at least as honest about consequences as the CLI, not
less. This is a safety regression hiding inside the nicer UI.

## Suggested approach

1. When entering confirm mode, compute the plan once and stash it on the model:

   ```go
   case "x":
       if s, ok := m.current(); ok {
           m.selected = s
           m.plan = kill.Preview(s, m.killOpts()) // new field on Model
           m.mode = modeConfirm
       }
   ```

2. Render the tree in the confirm box, reusing the same shape as the CLI's
   `printPlan` (consider lifting that formatter into a shared spot so the two
   surfaces stay identical):

   ```
   ┌─────────────────────────────────────────────┐
   │ Kill :3000 myapp — 3 processes?              │
   │   12345 make   (tree root)                   │
   │   12346 node                                 │
   │   12347 esbuild                              │
   │ [y/N]                                        │
   └─────────────────────────────────────────────┘
   ```

   For a Docker target show `docker stop`; for a no-PID row show the
   "owned by another user" note — same three cases `printPlan` already handles.

3. While you're here, give the TUI a single-process kill (the CLI's `--single`):
   bind a key (e.g. `X` or a toggle in the confirm modal) so a user can choose
   "just the listener" vs. "the whole tree." `kill.Opts.Single` already exists;
   the TUI just never sets it.

## Tests / verification

- The TUI tests already pump `Update()` (`tui_test.go`). Add a case: load a
  server, press `x`, assert `m.plan.Tree` is populated and the rendered
  `View()` contains every PID in the tree.
- Manual: start `make dev` that launches two servers, open the TUI, press `x` on
  one — confirm the modal lists both.

## Notes / trade-offs

- `kill.Preview` takes a fresh process snapshot; computing it on keypress (not on
  every render) keeps it cheap. The existing note on `Preview` — that the tree
  may shift slightly between preview and kill — applies equally here and is fine.
- Lifting `printPlan`/`describe` into a shared package (e.g. `internal/output`
  or a `kill.FormatPlan`) avoids the two confirmations drifting apart later,
  which is the whole point of the single-`planTree` design.
