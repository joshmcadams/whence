package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/joshmcadams/whence/internal/config"
)

func TestConfigInit_WritesFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out bytes.Buffer
	if err := runConfig(&out, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "wrote default config to") {
		t.Errorf("output = %q, want the 'wrote default config' message", out.String())
	}
}

func TestConfigInit_RefusesWhenFileExists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out bytes.Buffer
	if err := runConfig(&out, true); err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	origInfo, err := os.Stat(config.Path())
	if err != nil {
		t.Fatalf("stat after init: %v", err)
	}
	origModTime := origInfo.ModTime()

	var out2 bytes.Buffer
	err = runConfig(&out2, true)
	if err == nil {
		t.Fatal("want error when config already exists, got nil")
	}
	if !strings.Contains(err.Error(), "config already exists") {
		t.Errorf("err = %q, want it to mention 'config already exists'", err.Error())
	}

	info, err := os.Stat(config.Path())
	if err != nil {
		t.Fatalf("stat after refused init: %v", err)
	}
	if !info.ModTime().Equal(origModTime) {
		t.Error("config file was modified by the refused init call")
	}
}

func TestConfigInit_CreateOnlyOnce(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out bytes.Buffer
	if err := runConfig(&out, true); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := runConfig(new(bytes.Buffer), true); err == nil {
		t.Fatal("second init must refuse, got nil")
	}
}

func TestConfigFlags(t *testing.T) {
	cmd := newConfigCmd()
	if cmd.Flags().Lookup("init") == nil {
		t.Error("missing flag 'init'")
	}
}
