//go:build windows

package kill

import (
	"strconv"
	"time"

	"github.com/joshmcadams/whence/internal/execx"
)

// taskkillTimeout bounds a taskkill invocation so a hung call can't stall the
// kill loop.
const taskkillTimeout = 10 * time.Second

// Windows has no POSIX signals. taskkill without /F asks the process to close
// (only reliably honored by GUI apps); /F forces termination. Graceful
// shutdown of console dev servers is therefore best-effort on Windows.
func terminate(pid int) error {
	return execx.Run(taskkillTimeout, "taskkill", "/PID", strconv.Itoa(pid))
}

func forceKill(pid int) error {
	return execx.Run(taskkillTimeout, "taskkill", "/F", "/PID", strconv.Itoa(pid))
}
