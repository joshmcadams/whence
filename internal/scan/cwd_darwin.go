//go:build darwin

package scan

import (
	"fmt"
	"strings"
	"time"

	"github.com/joshmcadams/whence/internal/execx"
)

// lsofTimeout bounds a single lsof call so one stuck process can't stall the
// whole scan; on timeout the caller records it as a per-row note.
const lsofTimeout = 2 * time.Second

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
	out, err := execx.Output(lsofTimeout, "lsof", "-a", "-p", fmt.Sprint(pid), "-d", "cwd", "-Fn")
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
