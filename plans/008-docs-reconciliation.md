# Plan 008: Reconcile stale docs — backlog, DESIGN.md, README, phase comments, kill-climb caveat

> **Executor instructions**: Follow this plan step by step. This plan is
> docs/comments only — zero runtime changes. On any STOP condition, stop and
> report. When done, update this plan's status row in `plans/README.md` —
> unless a reviewer told you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- backlog DESIGN.md README.md AGENTS.md internal/kill/AGENTS.md internal/model/model.go internal/config/config.go internal/scan/cwd_windows.go`
> Plan 007 should be DONE first (it rewrites the AGENTS.md CI sentence; this
> plan must not undo it).

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (docs and comments only)
- **Depends on**: plans/007-toolchain-and-ci.md
- **Category**: docs
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

The backlog is this repo's de facto work index — AGENTS.md sends readers to
it — and it currently points at finished work: imp-06…09 and item 05 are all
shipped on main but still marked todo, and `05-review-followups.md` describes
dual-stack behavior that `AGENTS.md` now lists as an invariant NOT to revert.
DESIGN.md describes the pre-collapse dedup as current design, wrong TUI
columns/keys, and wrong default dev roots. "Phase N" scaffolding comments in
`model`/`config` claim features are unimplemented that shipped long ago. And
`internal/kill/AGENTS.md` documents a climb behavior (`npm run dev` climbs to
npm) that in reality stops at the `sh` npm interposes. Stale-and-wrong docs
actively mislead the humans and agents that work here.

## Current state (each item verified against code at `caec51a`)

1. `backlog/README.md:12-23` — items table: 01 listed pending though
   `backlog/01-add-remote-and-push.md:3` says done; 05 listed open though
   commit `c864652` resolved both trade-offs.
2. `backlog/README.md:34-47` — imp table: imp-06…09 have no ✅ and carry
   effort estimates, but all four shipped (commits `ca51dfe`, `0a591c2`,
   `49802a5`, `312965b`); closing paragraph recommends prioritizing
   already-finished work. Item files `imp-06…imp-09` each still say
   `**Status:** todo`; `imp-01…05` say `done (branch improvements)` though
   merged to main.
3. `backlog/05-review-followups.md` — says todo; body describes dual-stack
   servers as showing two rows, contradicted by `scan.collapseIPv4IPv6` and
   AGENTS.md's invariant.
4. `DESIGN.md:30` — "dedupe by `(proto, port)`. IPv4 + IPv6" (superseded by
   the collapse); `DESIGN.md:79-99` — `Server` struct omits `Source`, `Name`,
   `Notes`, `Address`; `DESIGN.md:121` — "`whence config` # show / edit
   config path" (edit doesn't exist — see plan 020); `DESIGN.md:125` —
   default dev roots list doesn't match `config.Default()`
   (`internal/config/config.go:38-48`: no `~/Development`; has
   `projects`/`Code`/`work`); `DESIGN.md:141-151` — TUI columns are actually
   PORT/PROTO/UPTIME/SRC/SERVER/DESCRIPTION (`internal/tui/tui.go:462-472`)
   and the key table omits `t`/`esc`; `DESIGN.md:200` — stray trailing code
   fence as the last line.
5. `README.md:101-108` — config example shows 3 dev roots as if they were the
   scaffold, actual default has 9; `README.md:56-77` — Usage omits
   `list --interval` (`internal/cli/list.go:44`), `kill --timeout`
   (`internal/cli/kill.go:42`), `tui --all` (`internal/cli/tui.go:27`).
6. Stale phase comments: `internal/model/model.go:2` ("and (later) the TUI"),
   `model.go:12` ("(Phase 2)"), `model.go:57` ("populated in Phase 2");
   `internal/config/config.go:14-15` ("scaffolded now and consumed in later
   phases"), `config.go:23` ("(Phase 3)"), `config.go:26` ("(Phase 2)");
   `internal/scan/cwd_windows.go:12-13` ("must be verified … during the
   Phase-1 spike" — backlog item 04 now tracks this).
7. `internal/kill/AGENTS.md:19-22` claims the npm/yarn/pnpm chain is climbed.
   In reality `npm run dev` executes scripts via `sh -c`, so the tree is
   `npm → sh → node(listener)` and `climb` stops at `sh` (shells are
   deliberately not launchers). Same for `make` recipes with shell
   metacharacters. The behavior is correct-by-invariant; the doc is wrong.
   **Decision recorded in plans/README.md**: fix the doc, do NOT change the
   climb behavior.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Compile (comments only) | `go build ./...` | exit 0 |
| Full suite | `make test` | ok |
| Lint | `make lint` | exit 0 |

## Scope

**In scope**:
- `backlog/README.md`, `backlog/01-add-remote-and-push.md`,
  `backlog/05-review-followups.md`, `backlog/imp-01…imp-09` (status lines)
- `DESIGN.md`, `README.md`
- `AGENTS.md` (only if a stale line remains after plan 007 — do not rewrite 007's work)
- `internal/kill/AGENTS.md`
- Comment-only edits: `internal/model/model.go`, `internal/config/config.go`,
  `internal/scan/cwd_windows.go`

**Out of scope**:
- ANY code change beyond comments. `git diff` on .go files must show comment
  lines only.
- Deleting backlog files (mark done; history is the point of the folder).
- README feature docs for things plans 019/020 will add — document current
  reality only.

## Git workflow

- Branch: `advisor/008-docs-reconcile`
- Commit style: `docs: ...` prefix.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Backlog statuses

- `backlog/README.md`: mark 01 and 05 done in the items table (✅ + "done");
  update the "Suggested order" line to only reference still-open items
  (02, 03, 04); mark imp-06…09 ✅ done in the imp table; rewrite the closing
  paragraph ("imp-01 → imp-04 are the highest-leverage…") to state all imp
  items are complete and point at `plans/README.md` for current work.
- Flip `**Status:** todo` → `**Status:** done (see commit <sha>)` in
  `imp-06…imp-09` and `05-review-followups.md`; change imp-01…05's
  `done (branch improvements)` → `done (merged to main)`.
- In `05-review-followups.md`, add a short "Resolution" note at top: item 1
  resolved by collapsing dual-stack rows (`scan.collapseIPv4IPv6`, commit
  `c864652` — now an AGENTS.md invariant); item 2 resolved by keeping
  case-insensitive matching with a clarifying comment in
  `internal/config/config.go`. Leave the original body for history.

**Verify**: `grep -rn "Status:\s*todo" backlog/` → no matches.

### Step 2: DESIGN.md

Add a banner directly under the H1:

```markdown
> **Historical design document (June 2026).** This captures the original
> plan and rationale. Where it disagrees with the code or `AGENTS.md`,
> they win — notably: dual-stack sockets now collapse to one row
> (`scan.collapseIPv4IPv6`), the `Server` model gained `Source`/`Name`/
> `Address`/`Notes`, and the TUI columns/keys evolved. See `AGENTS.md`
> for current invariants.
```

Then two spot-fixes (the ones that contradict a live invariant or would send
someone to a wrong config): annotate the `DESIGN.md:30` dedup sentence with
"(superseded — see banner)"; same one-liner next to the dev-roots default
list at `DESIGN.md:125`. Delete the stray trailing code fence (last line).
Do NOT rewrite the whole document — the banner covers the rest.

**Verify**: `tail -3 DESIGN.md` shows prose, not a bare ``` fence; banner
present under the title.

### Step 3: README

- Replace the config example dev_roots line with the real scaffold (read
  `internal/config/config.go:38-48` and render the 9 defaults with
  `/home/you` as the home placeholder), or keep 3 entries and append a
  comment line `# default also includes ~/Projects, ~/projects, ~/code, ~/Code, ~/work, ~/go/src` —
  pick whichever keeps the block honest AND short.
- Add to the Usage block: `whence list --watch --interval 5s`,
  `whence kill 3000 --timeout 10s`, `whence tui --all` — one line each with
  the same comment style as neighbors.

**Verify**: every flag shown in README Usage exists in `internal/cli` (cross-check
`grep -n 'Flags()' -A8 internal/cli/*.go` output against the README block).

### Step 4: Stale code comments (comment-only edits)

- `internal/model/model.go`: drop "and (later) the TUI" (the TUI exists);
  drop "(Phase 2)" at line 12 and "populated in Phase 2" at line 57 (say
  what's true instead: "populated by the docker detection path" /
  "attached by classify").
- `internal/config/config.go`: delete the "scaffolded now and consumed in
  later phases" sentence (all fields are consumed); drop "(Phase 3)"/
  "(Phase 2)" parentheticals.
- `internal/scan/cwd_windows.go`: replace the "Phase-1 spike" sentence with a
  pointer to `backlog/04-verify-macos-windows.md`.

**Verify**: `grep -rn "Phase" internal/ --include='*.go'` → no matches;
`go build ./... && make test` → exit 0 (comments can't break it, but prove it).

### Step 5: kill-climb caveat

In `internal/kill/AGENTS.md`, rewrite the npm/yarn/pnpm paragraph to describe
actual behavior: interactive shells are never climbed through, and because
`npm run` / `make` recipes usually interpose `sh -c`, the climb from the
listener typically stops at that shell — the wrapper above it is NOT killed;
the confirmation preview always shows the true tree, so what you see is what
dies. Note the direct-exec case (wrapper spawns the server without a shell)
still climbs as the launchers list describes. Mirror a one-line correction in
root `AGENTS.md`'s "kill <port> can terminate more than the named port"
caveat if it repeats the claim (read it; adjust only if wrong).

**Verify**: `make lint && make test` → exit 0.

## Test plan

None (docs). The verification greps above are the gates.

## Done criteria

- [ ] `grep -rn "Status:\s*todo" backlog/` → empty
- [ ] DESIGN.md has the historical banner and no trailing bare fence
- [ ] README Usage documents `--interval`, `kill --timeout`, `tui --all`; config example matches (or honestly summarizes) `config.Default()`
- [ ] `grep -rn "Phase" internal/ --include='*.go'` → empty
- [ ] `internal/kill/AGENTS.md` no longer claims npm is climbed through an interposed shell
- [ ] `git diff caec51a..HEAD -- '*.go'` shows comment-line changes only
- [ ] `make lint && make test` exit 0; `plans/README.md` row updated

## STOP conditions

- Plan 007 is not DONE (its AGENTS.md sentence must land first).
- You find a doc claim whose truth you cannot determine from the code (don't
  guess — list it in the report).
- Any .go diff line that isn't a comment.

## Maintenance notes

- If plans 019/020 land later, README gains their features in those plans —
  don't pre-document here.
- The backlog folder is now historical except items 02–04; new work tracking
  lives in `plans/README.md`. Consider (operator decision, not this plan)
  adding that pointer to AGENTS.md's "Off-machine work" section.
