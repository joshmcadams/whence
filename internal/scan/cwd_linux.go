//go:build linux

package scan

import (
	"fmt"
	"os"
)

// processCwd reads /proc/<pid>/cwd. Works for own-user processes without
// privileges; other users' processes return a permission error (recorded as a
// note by the caller). Also covers WSL, which exposes a standard /proc.
func processCwd(pid int32) (string, error) {
	link := fmt.Sprintf("/proc/%d/cwd", pid)
	cwd, err := os.Readlink(link)
	if err != nil {
		if os.IsPermission(err) {
			return "", fmt.Errorf("permission denied: %w", err)
		}
		return "", err
	}
	return cwd, nil
}

// processCwds resolves cwds pid-by-pid via /proc on Linux. Each per-pid
// error (permission, ENOENT) is preserved in the result so enrich can write
// the usual cwd: notes.
func processCwds(pids []int32) map[int32]cwdResult {
	out := make(map[int32]cwdResult, len(pids))
	for _, pid := range pids {
		path, err := processCwd(pid)
		out[pid] = cwdResult{path: path, err: err}
	}
	return out
}
