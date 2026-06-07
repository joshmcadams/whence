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
			return "", fmt.Errorf("permission denied")
		}
		return "", err
	}
	return cwd, nil
}
