//go:build darwin

package scan

import (
	"fmt"
	"os/exec"
	"strings"
)

// processCwd resolves a process's working directory on macOS, where gopsutil
// does not implement Cwd(). We parse `lsof` field output:
//
//	lsof -a -p <pid> -d cwd -Fn
//
// emits one line per field; the cwd path is the line beginning with 'n'.
// NOTE: this makes lsof a runtime dependency on macOS (doctor reports it).
// A cgo proc_pidinfo(PROC_PIDVNODEPATHINFO) implementation can replace this
// later to drop the dependency.
func processCwd(pid int32) (string, error) {
	out, err := exec.Command("lsof", "-a", "-p", fmt.Sprint(pid), "-d", "cwd", "-Fn").Output()
	if err != nil {
		return "", fmt.Errorf("lsof: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "n") {
			return strings.TrimPrefix(line, "n"), nil
		}
	}
	return "", fmt.Errorf("cwd not found in lsof output")
}
