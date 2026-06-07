# 01 — Create the GitHub repo, add the remote, push

**Status:** todo
**Blocks:** all other backlog items (CI, releases, installs)
**Owner:** you (requires your GitHub auth)

## Why

The repo is initialized locally (branch `main`, commits in place) but has no
remote. CI (`.github/workflows/ci.yml`) only runs once the code is on GitHub, and
GoReleaser needs the remote to determine release refs.

## Prerequisites

- A GitHub account `joshmcadams`.
- `gh` CLI authenticated (`gh auth status`) **or** a repo created in the web UI.

## Steps

Using the `gh` CLI (creates the repo and pushes in one step):

```sh
cd ~/development/personal/ports
gh repo create joshmcadams/ports --public --source=. --remote=origin --push
```

Or manually:

```sh
cd ~/development/personal/ports
# create an empty repo named "ports" at github.com/joshmcadams first, then:
git remote add origin https://github.com/joshmcadams/ports.git
git push -u origin main
```

## Verify

- `git remote -v` shows `origin` → `github.com/joshmcadams/ports`.
- The **CI** workflow runs on the push and goes green (Actions tab): `go vet`,
  `go test`, the cross-build matrix, and `goreleaser check`.

## Notes

- If you pick a repo name other than `ports`, also update the Go module path in
  `go.mod` and the `-X github.com/joshmcadams/ports/internal/cli.version` ldflags
  path in `.goreleaser.yaml` (search the tree for `joshmcadams/ports`).
