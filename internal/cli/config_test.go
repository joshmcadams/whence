package cli

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/joshmcadams/whence/internal/config"
)

// setEditor points --edit at a stub editor for the test, clearing $VISUAL so
// the dev box's real editor can't take precedence, and skips when the stub
// binary isn't on PATH (e.g. Windows has no true/false).
func setEditor(t *testing.T, editor string) {
	t.Helper()
	bin := strings.Fields(editor)[0]
	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("%s not on PATH; skipping", bin)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", editor)
}

func TestConfigInit_WritesFile(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out bytes.Buffer
	if err := runConfig(&out, true, false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "wrote default config to") {
		t.Errorf("output = %q, want the 'wrote default config' message", out.String())
	}
}

func TestConfigInit_RefusesWhenFileExists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out bytes.Buffer
	if err := runConfig(&out, true, false); err != nil {
		t.Fatalf("first init failed: %v", err)
	}

	origInfo, err := os.Stat(config.Path())
	if err != nil {
		t.Fatalf("stat after init: %v", err)
	}
	origModTime := origInfo.ModTime()

	var out2 bytes.Buffer
	err = runConfig(&out2, true, false)
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
	if err := runConfig(&out, true, false); err != nil {
		t.Fatalf("first init: %v", err)
	}
	if err := runConfig(new(bytes.Buffer), true, false); err == nil {
		t.Fatal("second init must refuse, got nil")
	}
}

func TestConfigFlags(t *testing.T) {
	cmd := newConfigCmd()
	if cmd.Flags().Lookup("init") == nil {
		t.Error("missing flag 'init'")
	}
	if cmd.Flags().Lookup("edit") == nil {
		t.Error("missing flag 'edit'")
	}
}

func TestConfigEdit_InitAndEditExclusive(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	err := runConfig(new(bytes.Buffer), true, true)
	if err == nil {
		t.Fatal("expected error for --init --edit together")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("err = %q, want mutually exclusive message", err.Error())
	}
}

func TestConfigEdit_CreatesWhenAbsent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	setEditor(t, "true")

	var out bytes.Buffer
	if err := runConfig(&out, false, true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "wrote default config to") {
		t.Errorf("output = %q, want 'wrote default config'", out.String())
	}
	if _, err := os.Stat(config.Path()); err != nil {
		t.Fatalf("config file should exist after --edit: %v", err)
	}
}

func TestConfigEdit_EditorError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := config.Save(config.Default())
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	setEditor(t, "false")

	err = runConfig(new(bytes.Buffer), false, true)
	if err == nil {
		t.Fatal("expected error when editor exits non-zero")
	}
	if !strings.Contains(err.Error(), "exited with error") {
		t.Errorf("err = %q, want 'exited with error'", err.Error())
	}
}

func TestConfigEdit_EditorWithArguments(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	// Regression: an $EDITOR carrying arguments (the "code --wait" shape) must
	// be split into binary + args, not treated as one binary name.
	setEditor(t, "true --wait")

	if err := runConfig(new(bytes.Buffer), false, true); err != nil {
		t.Fatalf("EDITOR with arguments should work: %v", err)
	}
}
