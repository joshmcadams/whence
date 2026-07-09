//go:build !windows

package project

import (
	"path/filepath"
	"syscall"
	"testing"
)

// TestReadSmallFile_FIFORejected proves readSmallFile never calls os.ReadFile
// on a non-regular file: a FIFO with no reader/writer on the other end would
// block os.ReadFile forever, which would hang every `whence list` on a
// hostile repo. Lstat's mode check must reject it before the read is
// attempted. Run with -timeout to make a regression fail fast instead of
// hanging the whole test binary.
func TestReadSmallFile_FIFORejected(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "README.md")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skipf("mkfifo unsupported: %v", err)
	}

	data, ok := readSmallFile(path)
	if ok || data != nil {
		t.Fatalf("readSmallFile on FIFO = (%v, %v), want (nil, false)", data, ok)
	}
}
