# 02 — Create the Homebrew tap + Scoop bucket and CI secrets

**Status:** todo
**Blocks:** `brew install` and `scoop install` (item 03 still produces raw
binaries without this)
**Owner:** you (requires creating repos + secrets under `joshmcadams`)

## Why

GoReleaser is configured to publish a Homebrew **cask** and a Scoop **manifest**
on every release (`.goreleaser.yaml` → `homebrew_casks:` and `scoops:`). It pushes
those to separate repos using tokens. Without the repos and tokens, the release
will still build binaries/archives but the cask/scoop publishing step will fail.

If you don't care about brew/scoop yet, you can **skip this** and delete (or
comment out) the `homebrew_casks:` and `scoops:` sections in `.goreleaser.yaml`;
users can still download binaries from the releases page or `go install`.

## Steps

1. **Create the two repos** (must be public for users to install from):
   - `github.com/joshmcadams/homebrew-tap`
   - `github.com/joshmcadams/scoop-bucket`

   ```sh
   gh repo create joshmcadams/homebrew-tap --public -d "Homebrew tap for joshmcadams tools"
   gh repo create joshmcadams/scoop-bucket --public -d "Scoop bucket for joshmcadams tools"
   ```

2. **Create a fine-grained Personal Access Token** with **Contents: read/write**
   on those two repos (or a classic token with `repo` scope). GitHub:
   Settings → Developer settings → Personal access tokens.

3. **Add the token as secrets** on the `ports` repo (the release workflow reads
   both names; the same token can back both):

   ```sh
   gh secret set HOMEBREW_TAP_GITHUB_TOKEN --repo joshmcadams/ports
   gh secret set SCOOP_BUCKET_GITHUB_TOKEN --repo joshmcadams/ports
   ```

## Verify

After the first release (item 03):
- `github.com/joshmcadams/homebrew-tap` contains `Casks/ports.rb`.
- `github.com/joshmcadams/scoop-bucket` contains `ports.json`.
- `brew install --cask joshmcadams/tap/ports` works on macOS.
- `scoop bucket add joshmcadams https://github.com/joshmcadams/scoop-bucket && scoop install ports` works on Windows.

## Notes

- The default `GITHUB_TOKEN` in Actions **cannot** push to other repos, which is
  why a separate PAT is required for the tap/bucket.
- The cask includes a quarantine-removal hook so macOS won't flag the unsigned
  binary as "damaged". Proper notarization/signing is a future nice-to-have.
