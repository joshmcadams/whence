//go:build !windows

package config

func configBase() string {
	return xdgWhenceDir()
}
