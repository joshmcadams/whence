//go:build windows

package scan

import "github.com/shirou/gopsutil/v4/process"

// processCwd uses gopsutil's Windows implementation, which reads the target
// process's PEB (ProcessParameters.CurrentDirectory) via NtQueryInformationProcess.
// Reading another process's memory across the 32/64-bit boundary can fail
// without sufficient rights; the caller records that as a note.
//
// This path is NOT exercised by the Linux/WSL dev box and must be verified on a
// real Windows host during the Phase-1 spike.
func processCwd(pid int32) (string, error) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return "", err
	}
	return p.Cwd()
}
