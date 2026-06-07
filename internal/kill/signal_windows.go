//go:build windows

package kill

import (
	"os/exec"
	"strconv"
)

// Windows has no POSIX signals. taskkill without /F asks the process to close
// (only reliably honored by GUI apps); /F forces termination. Graceful
// shutdown of console dev servers is therefore best-effort on Windows.
func terminate(pid int) error {
	return exec.Command("taskkill", "/PID", strconv.Itoa(pid)).Run()
}

func forceKill(pid int) error {
	return exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid)).Run()
}
