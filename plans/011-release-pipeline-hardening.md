# Plan 011: Release pipeline hardening — CI permissions, SHA-pinned actions, reproducible releases

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- .github/workflows .goreleaser.yaml README.md`
> Plan 007 also edits `ci.yml` (lint/toolchain). Either order works, but
> rebase carefully — this plan only ADDS a permissions block, SHA pins, and a
> tidiness step to `ci.yml`.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (config-only; release behavior changes are deliberate and listed)
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

The release path is the highest-privilege surface this repo has: the
goreleaser action receives tokens with write access to the tap and bucket
repos that end users install from. Today: CI jobs run with the repository's
default `GITHUB_TOKEN` grants (no `permissions:` block); every third-party
action is pinned by *mutable tag* (`@v4`, `@v5`, `@v6`, `@v7`), so a retagged
or compromised upstream action executes with those grants — on release, with
the cross-repo publish tokens; `go mod tidy` runs *at release time*, so the
shipped binary may not correspond to the committed go.mod/go.sum; and the
Homebrew cask strips the macOS quarantine attribute, disabling
Gatekeeper/notarization checks for every tap install — so a tampered release
would install with no OS-level prompt at all.

## Current state

- `.github/workflows/ci.yml` — no `permissions:` at any level. Actions:
  `actions/checkout@v4`, `actions/setup-go@v5` (×3),
  `golangci/golangci-lint-action@v7`, `goreleaser/goreleaser-action@v6`
  (the `args: check` one in the test job).
- `.github/workflows/release.yml` — `permissions: contents: write` (needed
  for release assets); `actions/checkout@v4` (fetch-depth 0),
  `actions/setup-go@v5`, `goreleaser/goreleaser-action@v6` with env
  `GITHUB_TOKEN`, `HOMEBREW_TAP_GITHUB_TOKEN`, `SCOOP_BUCKET_GITHUB_TOKEN`.
- `.goreleaser.yaml`:

  ```yaml
  before:
    hooks:
      - go mod tidy
      - go test ./...
  ```

  and under `homebrew_casks[0]`:

  ```yaml
  # Remove the macOS quarantine attribute so the binary runs without a Gatekeeper prompt.
  hooks:
    post:
      install: |
        if system_command("/usr/bin/xattr", args: ["-h"]).exit_status == 0
          system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/whence"]
        end
  ```

- `README.md:43-47` — Homebrew install section (`brew install --cask ...`).
- No release has been cut yet (backlog item 03 is open), so these changes
  break no existing consumer.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Resolve a tag to a SHA | `gh api repos/actions/checkout/git/ref/tags/v4.2.2 --jq .object.sha` (see step 2 for the exact tags) | 40-hex SHA |
| Goreleaser config check | `goreleaser check` if installed, else skip (CI runs it) | "1 configuration file(s) validated" |
| Tidiness | `go mod tidy && git diff --exit-code go.mod go.sum` | exit 0 |
| Suite | `make lint && make test` | exit 0 |

## Scope

**In scope**:
- `.github/workflows/ci.yml` (permissions block, SHA pins, tidiness step)
- `.github/workflows/release.yml` (SHA pins)
- `.goreleaser.yaml` (drop `go mod tidy` hook; remove the quarantine hook)
- `README.md` (one note in the Homebrew section about the Gatekeeper prompt)

**Out of scope**:
- Signing/notarizing macOS binaries — deferred (needs an Apple Developer ID);
  recorded in `plans/README.md` rejected/deferred list.
- The set of release targets, archives, tap/bucket owners.
- Plan 007's lint-job changes — don't undo them if present.

## Git workflow

- Branch: `advisor/011-release-hardening`
- Commit style: `ci: ...` prefix.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Least-privilege CI token

At the top of `ci.yml` (workflow level, after `on:`):

```yaml
permissions:
  contents: read
```

`release.yml` keeps `contents: write` (goreleaser uploads release assets) —
unchanged.

### Step 2: Pin actions to commit SHAs

For each action reference in BOTH workflows, resolve the currently-used major
tag to its latest release tag and that tag's commit SHA, then pin:

```yaml
- uses: actions/checkout@<40-hex-sha> # v4.x.y
```

Resolution procedure per action (needs network; see STOP if unavailable):
`gh api repos/<owner>/<repo>/releases/latest --jq .tag_name` → take that tag →
`gh api repos/<owner>/<repo>/git/ref/tags/<tag> --jq '.object.sha'`. If the
ref object is an annotated tag (`.object.type == "tag"`), dereference once:
`gh api repos/<owner>/<repo>/git/tags/<that-sha> --jq .object.sha`. Constrain
to the SAME MAJOR as today (checkout v4, setup-go v5, golangci-lint-action
v7, goreleaser-action v6) — do not take a major bump in this plan. Keep the
human-readable version in a trailing comment.

Actions to pin: `actions/checkout` (3× in ci.yml, 1× release.yml),
`actions/setup-go` (3× + 1×), `golangci/golangci-lint-action` (1×),
`goreleaser/goreleaser-action` (1× ci.yml `check`, 1× release.yml).

### Step 3: Reproducible releases

- In `.goreleaser.yaml`, delete the `- go mod tidy` hook line (keep
  `- go test ./...`).
- In `ci.yml`'s `test` job, add a step before `go test`:

  ```yaml
  - name: go.mod is tidy
    run: go mod tidy && git diff --exit-code go.mod go.sum
  ```

- Run `go mod tidy && git diff --exit-code go.mod go.sum` locally now; if it
  produces a diff, REVERT the local change and report it (the tree isn't
  tidy at base — operator decides whether to commit the tidy separately).

### Step 4: Remove the quarantine-strip hook

Delete the whole `hooks:` block under `homebrew_casks[0]` in
`.goreleaser.yaml` (the comment line too). In README's Homebrew section, add:

```markdown
> macOS will show a one-time Gatekeeper prompt on first run because the
> binary is not notarized (right-click → Open, or
> `xattr -d com.apple.quarantine $(which whence)` if you accept the binary).
```

### Step 5: Validate

`goreleaser check` if the binary is installed (`command -v goreleaser`);
otherwise rely on CI's existing `goreleaser check` step and read the YAML
through once. `make lint && make test` → exit 0.

## Test plan

No Go tests. Gates: `goreleaser check` (locally or first CI run), the
tidiness command, and YAML review.

## Done criteria

- [ ] `ci.yml` starts with `permissions: contents: read`
- [ ] `grep -En 'uses: .+@v[0-9]+\s*$' .github/workflows/*.yml` → no matches (every `uses:` is a 40-hex SHA with a version comment)
- [ ] `.goreleaser.yaml` has no `go mod tidy` hook and no cask `hooks:` block
- [ ] `ci.yml` test job checks tidiness; the command passes locally
- [ ] README Homebrew section documents the Gatekeeper prompt
- [ ] `make lint && make test` exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- No network access to resolve tag→SHA (the `gh api` calls fail): apply
  Steps 1, 3, 4, 5 and report Step 2 as blocked with the exact commands the
  operator should run.
- `go mod tidy` dirties go.mod/go.sum at base (see Step 3) — report, don't
  commit the tidy inside this plan.
- The operator has expressed (anywhere in repo docs) a preference for keeping
  the quarantine strip — none found at planning time, but if you find one,
  skip Step 4 and report.

## Maintenance notes

- SHA pins go stale; bumping them is now a deliberate act (consider
  Dependabot/Renovate later — out of scope here).
- If notarization is ever added (Apple Developer ID), that supersedes the
  README Gatekeeper note.
- Reviewer: the quarantine-hook removal changes first-run UX for future brew
  users — confirm the README note ships in the same commit.
