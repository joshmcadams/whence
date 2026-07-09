//go:build windows

package scan

import "github.com/shirou/gopsutil/v4/process"

// processCwd uses gopsutil's Windows implementation, which reads the target
// process's PEB (ProcessParameters.CurrentDirectory) via NtQueryInformationProcess.
// Reading another process's memory across the 32/64-bit boundary can fail
// without sufficient rights; the caller records that as a note.
//
// This path is NOT exercised by the Linux/WSL dev box; verifying it on a real
// Windows host is tracked in backlog/04-verify-macos-windows.md.
func processCwd(pid int32) (string, error) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return "", err
	}
	return p.Cwd()
}
