//go:build windows

package config

import (
	"os"
	"path/filepath"
)

func configBase() string {
	if base := os.Getenv("AppData"); base != "" {
		return filepath.Join(base, "whence")
	}
	return xdgWhenceDir()
}
