# Plan 004: Recognize `.git` files (worktrees/submodules) in project root detection

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. On
> any STOP condition, stop and report. When done, update this plan's status
> row in `plans/README.md` — unless a reviewer told you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat caec51a..HEAD -- internal/project`
> On drift, compare the excerpt below to live code before proceeding.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW (broadens marker detection; monorepo anchoring unchanged)
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `caec51a`, 2026-07-09

## Why this matters

In a `git worktree` checkout (and in submodules), `.git` is a regular *file*
containing `gitdir: <path>`, not a directory. `findRoot` only accepts a `.git`
directory, so a server launched from a worktree skips the true repo root and
either falls back to a manifest or — worse — keeps walking up and anchors on an
*enclosing* repo's `.git` directory (a dotfiles repo at `$HOME`, or worktrees
kept inside another checkout), producing a wrong project name and root. That
wrong name then feeds `whence kill <name>` matching, so a name-kill can select
the wrong group of servers or miss the intended one. Agent-driven workflows use
worktrees constantly, so this is a realistic daily case.

## Current state

- `internal/project/project.go:76-98` — the walk:

  ```go
  func findRoot(start string) (root, marker string) {
      dir := filepath.Clean(start)
      var firstManifest, firstManifestMarker string
      for {
          if isDir(filepath.Join(dir, ".git")) {
              return dir, ".git"
          }
          ...
      }
      return firstManifest, firstManifestMarker
  }
  ```

- `internal/project/project.go:253-255` — helpers:

  ```go
  func exists(p string) bool { _, err := os.Stat(p); return err == nil }
  func isDir(p string) bool  { fi, err := os.Stat(p); return err == nil && fi.IsDir() }
  ```

- Tests: `internal/project/project_test.go` — white-box, same package, builds
  temp dirs with `t.TempDir()` and creates marker files/dirs by hand. Model
  new tests on the existing root-detection cases there.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Package tests | `go test ./internal/project/ -v` | pass |
| Full suite | `make test` | ok |
| Lint | `make lint` | exit 0 |

## Scope

**In scope**:
- `internal/project/project.go` (the one `isDir(.git)` call site)
- `internal/project/project_test.go`

**Out of scope**:
- The manifest fallback list and priority — unchanged.
- Following the `gitdir:` pointer to resolve the *main* repo — NOT wanted:
  the worktree directory itself is the correct project root for attribution
  (its cwd, its name via manifests). Do not read the `.git` file's contents.
- `internal/classify`, `internal/docker` — consumers, untouched.

## Git workflow

- Branch: `advisor/004-worktree-git-file`
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Accept `.git` as file or directory

In `findRoot`, replace the `isDir` check:

```go
if exists(filepath.Join(dir, ".git")) {
    return dir, ".git"
}
```

`exists` already lives in this file (project.go:253). Nothing else changes.
(A `.git` *file* is only ever created by git itself — worktrees and
submodules — so no false-positive concern worth extra validation.)

**Verify**: `go build ./... && go test ./internal/project/` → pass.

### Step 2: Tests

In `project_test.go`, add cases modeled on the existing findRoot tests:

1. **Worktree shape**: temp dir `repo/` containing a regular file `.git` with
   content `gitdir: /somewhere/else\n`, and a subdir `repo/web/`. Detect from
   `repo/web` → root is `repo`, marker `.git`.
2. **No over-walk**: outer temp dir with a real `.git` *directory*, inner dir
   `outer/wt/` with a `.git` *file*. Detect from `outer/wt/src` → root is
   `outer/wt` (the nearest marker), NOT `outer`.
3. **Regression guard**: existing `.git`-directory case still resolves
   (should already be covered by existing tests — confirm, don't duplicate).

**Verify**: `go test ./internal/project/ -v` → new tests pass;
`make lint && make test` → exit 0.

## Test plan

Covered in Step 2; pattern source: existing `t.TempDir()`-based tests in
`internal/project/project_test.go`.

## Done criteria

- [ ] `grep -n 'isDir(filepath.Join(dir, ".git"))' internal/project/project.go` returns no matches
- [ ] Worktree and no-over-walk tests exist and pass
- [ ] `make lint` and `make test` exit 0
- [ ] No files outside the in-scope list modified (`git status`)
- [ ] `plans/README.md` status row updated

## STOP conditions

- Existing project tests fail after the one-line change (would indicate a test
  deliberately pinning the directory-only behavior — surface it, don't delete it).
- You feel the need to parse the `.git` file contents — out of scope, report why.

## Maintenance notes

- If someone later wants monorepo-style grouping across worktrees (all
  worktrees of one repo sharing a project identity), that requires following
  `gitdir:` — a deliberate feature, not a bugfix; keep it out of any drive-by.
- Reviewer: check the no-over-walk test — that's the actual bug this fixes.
