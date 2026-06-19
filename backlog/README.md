# Backlog

Remaining work to get `whence` published and verified. These are the things that
must be done **outside this dev machine** (GitHub setup, releasing, and testing
on other operating systems).

The code is feature-complete (Phases 0–5), all tests pass, and it cross-compiles
for macOS/Linux/Windows. The GitHub user has been set to **`joshmcadams`**
throughout (module path, GoReleaser owners, README); the assumed repo name is
**`whence`** — adjust if you use a different name.

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

---

## Improvement suggestions (`imp-*`)

A review of the codebase (code, testing, UX, robustness) produced the following
suggestions, **ordered most-impactful first**. These are independent of the
release checklist above and can be done in any order; the numbering only reflects
priority. Each file is self-contained with file:line touch points and a suggested
approach.

| # | Item | Category | Effort |
|---|------|----------|--------|
| [imp-01](imp-01-timeout-external-commands.md) ✅ | Time-bound every external command (a hung Docker daemon hangs the default command) | robustness | done |
| [imp-02](imp-02-tui-kill-blast-radius.md) ✅ | Show the full kill process tree in the **TUI** confirmation (the CLI already does) | safety | done |
| [imp-03](imp-03-implement-ignore-lists.md) ✅ | Implement (or remove) `ignore_ports` / `ignore_names` — documented but dead config | correctness | done |
| [imp-04](imp-04-kill-by-name-exact-match.md) | Make `kill <name>` prefer exact matches over substring | safety | ~30 min |
| [imp-05](imp-05-test-untested-core-packages.md) | Test the 0%-coverage core packages (`config.IsUnderDevRoot`, `output`, `model`, `scan`) | testing | ~2 hr |
| [imp-06](imp-06-hidden-server-messaging.md) | Tell the user when servers are hidden instead of "nothing found" | UX | ~30 min |
| [imp-07](imp-07-surface-network-exposure.md) | Surface bind address / network exposure (the data is already collected) | UX | ~45 min |
| [imp-08](imp-08-eliminate-redundant-work.md) | Eliminate redundant work (cache project detection, share snapshot, overlap scan+docker) | perf | ~1–2 hr |
| [imp-09](imp-09-polish-grab-bag.md) | Polish grab-bag (version in `doctor`, JSON uptime, watch flicker, …) | polish | small |

The three at the top touch correctness/safety/robustness; **imp-01 → imp-04** are
the highest-leverage. The rest are quality, testing, and polish.
