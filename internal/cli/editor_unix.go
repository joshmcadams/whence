//go:build !windows

package cli

func fallbackEditor() string { return "vi" }
