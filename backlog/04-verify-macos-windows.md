# 04 — Smoke-test the working-directory paths on real macOS + Windows

**Status:** todo
**Priority:** do this **before** advertising macOS/Windows installs (item 02/03)
**Owner:** you (needs access to a Mac and a Windows box)

## Why

The whole tool hinges on resolving a process's working directory to map a port to
a repo. That resolution is **OS-specific** and only the Linux/WSL path has been
exercised on real hardware. The macOS and Windows implementations **compile and
cross-build cleanly** but have never actually run:

- `internal/scan/cwd_darwin.go` — shells out to `lsof` to read the cwd.
- `internal/scan/cwd_windows.go` — uses gopsutil's PEB read (`process.Cwd()`).

`gopsutil`'s socket enumeration on macOS may also rely on `lsof`/privileges. These
are exactly the things a Linux-only run cannot validate.

## Steps (run on each OS)

1. Build/install: `go install github.com/joshmcadams/whence/cmd/whence@latest`
   (or grab the release binary once item 03 is done).
2. Start a known dev server from a repo, e.g. `npm run dev` or
   `python3 -m http.server 8099` from inside a git repo under your dev root.
3. Run:
   ```sh
   whence doctor          # check capabilities/dependencies
   whence list            # the server should appear, attributed to its repo
   whence list --json     # confirm the "cwd" field is populated
   ```
4. Test a kill on a disposable server: `whence kill 8099`.

## What to check specifically

### macOS
- `whence doctor` shows `lsof` found. If missing, cwd resolution and possibly the
  whole scan degrade — confirm behavior and update `doctor` messaging if needed.
- `whence list` shows non-empty `DIRECTORY`/`cwd` and correct project attribution.
- Confirm whether socket enumeration needs `sudo` for your own processes (it
  shouldn't, but verify).

### Windows (native, not WSL)
- `whence list` resolves `cwd` for your own dev servers (the PEB read can fail
  across the 32/64-bit boundary or without rights — note what happens).
- `whence kill` uses `taskkill`; graceful shutdown is best-effort. Verify the
  port is actually freed (force path works) and that your shell isn't affected.
- Sanity-check that WSL servers do **not** appear here (separate netns) — and
  that running `whence` *inside* WSL shows the Linux side. This is expected.

## Acceptance

- On both OSes: a dev server started from a repo shows up attributed, and a
  disposable server can be killed with the port freed.
- Any gaps found (missing deps, permission needs, wrong messaging) are filed as
  follow-up items or fixed.

## If something's broken

- macOS cwd fallback: consider the cgo `proc_pidinfo(PROC_PIDVNODEPATHINFO)`
  implementation noted in `cwd_darwin.go` to drop the `lsof` dependency.
- Windows: if `process.Cwd()` is unreliable, a dedicated
  `NtQueryInformationProcess` helper may be needed.
