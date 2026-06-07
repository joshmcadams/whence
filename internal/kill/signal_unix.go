//go:build unix

package kill

import "syscall"

func terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

func forceKill(pid int) error { return syscall.Kill(pid, syscall.SIGKILL) }
