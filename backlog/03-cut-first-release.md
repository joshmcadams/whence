# 03 — Cut the first release (tag v0.1.0)

**Status:** todo
**Depends on:** 01 (remote pushed). Optionally 02 (for brew/scoop publishing).
**Owner:** you

## Why

`.github/workflows/release.yml` runs GoReleaser when a `v*` tag is pushed. It
builds binaries for macOS/Linux/Windows × amd64/arm64, creates archives +
checksums, publishes a GitHub Release, and (if item 02 is done) updates the tap
and bucket. The binary version comes from the tag via ldflags
(`whence --version`).

## Steps

1. Make sure CI is green on `main` (item 01).

2. (Optional) Dry-run a snapshot build locally to see artifacts without tagging:

   ```sh
   cd ~/development/personal/whence
   go run github.com/goreleaser/goreleaser/v2@latest release --snapshot --clean
   ls dist/
   ```

3. Tag and push:

   ```sh
   git tag -a v0.1.0 -m "whence v0.1.0"
   git push origin v0.1.0
   ```

4. Watch the **Release** workflow in the Actions tab.

## Verify

- A GitHub Release `v0.1.0` exists with archives for all 6 os/arch combos plus
  `checksums.txt`.
- A downloaded binary reports the right version: `whence --version` → `whence version 0.1.0`.
- If item 02 is done: the tap and bucket received their files.

## Notes

- Use semver tags going forward (`v0.1.1`, `v0.2.0`, …). The changelog is built
  from commit messages (commits prefixed `docs:`/`test:`/`chore:` are excluded).
- To delete a botched tag/release: `git push --delete origin v0.1.0` and remove
  the release in the GitHub UI, then re-tag.
