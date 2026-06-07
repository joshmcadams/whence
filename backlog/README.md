# Backlog

Remaining work to get `ports` published and verified. These are the things that
must be done **outside this dev machine** (GitHub setup, releasing, and testing
on other operating systems).

The code is feature-complete (Phases 0–5), all tests pass, and it cross-compiles
for macOS/Linux/Windows. The GitHub user has been set to **`joshmcadams`**
throughout (module path, GoReleaser owners, README); the assumed repo name is
**`ports`** — adjust if you use a different name.

| # | Item | Blocks | Effort |
|---|------|--------|--------|
| [01](01-add-remote-and-push.md) | Create the GitHub repo, add the remote, push | everything | 5 min |
| [02](02-publish-homebrew-and-scoop.md) | Create tap + bucket repos and CI secrets | brew/scoop installs | 15 min |
| [03](03-cut-first-release.md) | Tag `v0.1.0` and run the release | binaries/installs | 5 min |
| [04](04-verify-macos-windows.md) | Smoke-test the cwd paths on real macOS + Windows | correctness on those OSes | 30 min |
| [05](05-review-followups.md) | Two deferred design calls from the code review | nothing | 15 min |

Suggested order: **01 → 04 → 02 → 03** (verify it actually works on the other
OSes before you publish installable artifacts for them). Item **05** is
independent code-quality polish — do it whenever.
