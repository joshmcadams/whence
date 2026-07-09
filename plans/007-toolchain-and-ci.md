# Plan 007: Toolchain/CI cluster — go directive, pinned lint, gofmt enforcement, govulncheck

> **Executor instructions**: Follow this plan step by step, verifying each
> step before the next. On any STOP condition, stop and report. When done,
> update this plan's status row in `plans/README.md` — unless a reviewer told
> you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- go.mod .github/workflows/ci.yml .golangci.yml Makefile AGENTS.md README.md`
> On drift, compare excerpts below to live code before proceeding.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW (config/tooling only; one compile-level assumption to verify in Step 1)
- **Depends on**: none
- **Category**: dx + deps
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

Five related toolchain problems, one root cause:

1. `go.mod` declares `go 1.26.1` (patch-level). No code in the repo needs Go
   1.26 (audit grep found nothing newer than the Go 1.21 `max()` builtin), but
   every published golangci-lint binary through v2.12.2 is built with Go 1.25
   and refuses a 1.26 target — which forced CI to compile golangci-lint from
   source on every run (three commits of churn: `8a50f33`, `7239ce5`,
   `caec51a`), adding minutes to every push/PR.
2. CI uses `go-version: stable`, which silently drifts from the repo's pin.
3. gofmt is enforced nowhere in CI: golangci-lint v2 treats gofmt as a
   *formatter*, which `run` only checks when configured under `formatters:` —
   and `.golangci.yml` has no such section. Meanwhile `make lint` runs gofmt
   but only runs golangci-lint *if installed*, so each gate has a hole the
   other doesn't cover.
4. Local golangci-lint is version-unpinned and undocumented — a contributor
   installing it the normal way (brew/release binary) hits the same
   Go-version hard-fail CI did.
5. No vulnerability scanning exists anywhere (`govulncheck` absent from CI and
   the dev box) — advisories against gopsutil/cobra/the charm stack would go
   unnoticed.

## Current state

- `go.mod:3` — `go 1.26.1`.
- `.github/workflows/ci.yml` — three jobs (lint, test, cross-build), all with
  `actions/setup-go@v5` + `go-version: stable` + `cache: true`. Lint job:

  ```yaml
  - uses: golangci/golangci-lint-action@v7
    with:
      version: v2.12.2
      # No golangci-lint release is built with Go 1.26 yet, and a binary
      # built with an older Go refuses a module whose go directive is
      # 1.26. Build it from source with the setup-go toolchain instead.
      install-mode: goinstall
  ```

- `.golangci.yml` — `version: "2"`, default linter set, errcheck exclusions
  only. No `formatters:` block.
- `Makefile` lint target — gofmt check, `go vet`, then:

  ```make
  @if command -v golangci-lint >/dev/null 2>&1; then \
      golangci-lint run; \
  else \
      echo "golangci-lint not installed; ran gofmt + go vet only"; \
  fi
  ```

- `AGENTS.md:29-32` — claims CI runs "gofmt, `go vet`, `go test`, a
  cross-compile matrix …" (wrong: the lint job is golangci-lint only) and
  "Toolchain: Go 1.26+ (see `go.mod`)".
- `README.md:34` — "**From source** (Go 1.26+)".
- Today is 2026-07-09; the dev box has Go 1.26.1.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Compile under new directive | `go build ./...` | exit 0 |
| Full suite | `make test` | ok |
| Lint | `make lint` | exit 0 |
| Vuln scan (once wired) | `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | exit 0, "No vulnerabilities found" (or listed findings) |
| Workflow syntax | `git diff --check` + a YAML read-through | no errors |

## Scope

**In scope**:
- `go.mod` (the `go` directive line only — no dependency changes)
- `.github/workflows/ci.yml`
- `.golangci.yml`
- `Makefile` (lint target + one variable)
- `AGENTS.md` (toolchain + CI description lines), `README.md` (Go version line)

**Out of scope**:
- `.github/workflows/release.yml` and `.goreleaser.yaml` — plan 011's territory.
- Any dependency version in go.mod/go.sum.
- SHA-pinning actions (plan 011).

## Git workflow

- Branch: `advisor/007-toolchain-ci`
- Commit style: `ci: ...` / `build: ...` prefixes, matching recent history.
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Lower the `go` directive to the real minimum

1. Edit `go.mod:3` → `go 1.25`.
2. `go build ./... && go test ./...` — if both pass, 1.25 is the floor; done.
   If compilation fails with a language-version error, raise to the lowest
   version that passes (try `1.26` without the patch suffix as the ceiling)
   and record which construct forced it in the commit message.
   Note: do NOT add a `toolchain` line; the local 1.26.1 toolchain builds a
   `go 1.25` module fine.

**Verify**: `go build ./... && make test` → exit 0. `grep '^go ' go.mod` →
`go 1.25` (or the recorded minimum).

### Step 2: Restore the standard golangci-lint binary install

In `ci.yml`'s lint job, replace the `install-mode: goinstall` block:

```yaml
- uses: golangci/golangci-lint-action@v7
  with:
    version: v2.12.2
```

(Default install-mode downloads the released binary; with the directive at
1.25 the Go-1.25-built binary accepts the module.) Delete the stale comment.

Skip this step (and say so in the final report) if Step 1 ended above 1.25 —
in that case the goinstall workaround is still required and the right change
is only to add an explicit binary cache; see the fallback in "STOP conditions".

### Step 3: Pin CI's Go to the repo

In all three jobs of `ci.yml`, replace `go-version: stable` with
`go-version-file: go.mod` (keep `cache: true`).

### Step 4: Enforce gofmt in golangci-lint

In `.golangci.yml` add:

```yaml
formatters:
  enable:
    - gofmt
```

This makes `golangci-lint run` (CI and local) fail on unformatted code. The
tree is currently gofmt-clean, so nothing breaks today.

### Step 5: Pin and surface the local lint version

In `Makefile`, add near the top:

```make
GOLANGCI_VERSION := v2.12.2
```

and change the not-installed branch of `lint` to print an actionable hint:

```make
echo "golangci-lint not installed; ran gofmt + go vet only."; \
echo "install: go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION)"
```

(Deliberately still a soft warning, not a hard fail — CI is the enforcing
gate; local `make lint` staying runnable without extra installs is a feature.)

### Step 6: Add a govulncheck job to CI

Append to `ci.yml`:

```yaml
  vulncheck:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
          cache: true
      - run: go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Run the same command locally once; if it reports findings, list them verbatim
in your final report (do NOT fix dependencies in this plan).

### Step 7: True up the docs

- `AGENTS.md`: change the CI sentence to name what CI now actually runs
  ("golangci-lint (incl. gofmt via formatters), `go test`, govulncheck, a
  cross-compile matrix (linux/darwin/windows × amd64/arm64), and
  `goreleaser check`") and the toolchain line to the new floor ("Toolchain:
  Go 1.25+ (see `go.mod`); dev boxes on newer Go are fine").
- `README.md:34`: "**From source** (Go 1.25+):" (match Step 1's outcome).

**Verify**: `make lint && make test` → exit 0; read the final `ci.yml` top to
bottom once for YAML sanity (indentation, one `jobs:` key).

## Test plan

No Go tests — the verification gates are the tooling runs themselves plus the
first CI run after push (which the operator triggers, not you).

## Done criteria

- [ ] `grep '^go ' go.mod` shows the lowered directive
- [ ] `grep -n 'install-mode' .github/workflows/ci.yml` returns no matches (unless the Step-2 skip was taken — then say so)
- [ ] `grep -c 'go-version-file: go.mod' .github/workflows/ci.yml` ≥ 4 (three original jobs + vulncheck)
- [ ] `.golangci.yml` contains the `formatters:` block; `golangci-lint run` (if installed locally) exits 0
- [ ] `ci.yml` has a `vulncheck` job
- [ ] AGENTS.md/README.md updated lines match the new reality
- [ ] `go build ./... && make lint && make test` all exit 0
- [ ] `plans/README.md` status row updated

## STOP conditions

- Step 1 finds the code genuinely needs `go 1.26.x` (a compile error citing a
  language version). Fallback plan: keep the directive, keep
  `install-mode: goinstall`, and instead add an explicit binary cache to the
  lint job (`actions/cache` on `~/go/bin/golangci-lint` keyed on
  `golangci-lint-v2.12.2-go1.26`); apply Steps 3–7 unchanged. Report which
  construct forced the higher directive.
- `govulncheck` fails to run at all (network-restricted environment) — wire
  the CI job anyway, note the local run couldn't execute.
- Any test failure after only the go.mod change — that's a red flag about
  toolchain-dependent behavior; report before touching anything else.

## Maintenance notes

- When golangci-lint ships Go-1.26-built binaries, nothing needs to change
  here anymore — this plan removes the coupling by lowering the directive.
- Raising the `go` directive in the future re-creates the lint-binary
  constraint until golangci-lint catches up; check
  `golangci-lint version` build metadata before bumping.
- The govulncheck job uses `@latest` deliberately (scanner freshness beats
  reproducibility for an advisory scan); if CI flakiness appears, pin it.
- Reviewer: confirm the AGENTS.md CI sentence matches the final `ci.yml` —
  that doc is the contract agents follow (plan 008 fixes the rest of the
  stale docs and depends on this landing first).
