package execx

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// skipIfNoShellCmds skips tests that rely on unix coreutils (sleep/echo/false),
// which aren't available as plain binaries on Windows.
func skipIfNoShellCmds(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test relies on unix sleep/echo/false")
	}
}

func TestOutput_Success(t *testing.T) {
	skipIfNoShellCmds(t)
	out, err := Output(5*time.Second, "echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.TrimSpace(string(out)) != "hello" {
		t.Errorf("output = %q, want %q", out, "hello")
	}
}

func TestOutput_TimesOutPromptly(t *testing.T) {
	skipIfNoShellCmds(t)
	start := time.Now()
	_, err := Output(50*time.Millisecond, "sleep", "10")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, want it to mention a timeout", err)
	}
	// Should return right after the deadline, not after the full 10s sleep.
	if elapsed > 2*time.Second {
		t.Errorf("took %s to time out; expected ~50ms", elapsed)
	}
}

func TestRun_PropagatesCommandError(t *testing.T) {
	skipIfNoShellCmds(t)
	// `false` exits non-zero without timing out: a real failure, not a timeout.
	err := Run(5*time.Second, "false")
	if err == nil {
		t.Fatal("expected a non-zero exit error")
	}
	if strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %q, should not be reported as a timeout", err)
	}
}

func TestInteractive_Success(t *testing.T) {
	skipIfNoShellCmds(t)
	if err := Interactive("true"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInteractive_CommandError(t *testing.T) {
	skipIfNoShellCmds(t)
	err := Interactive("false")
	if err == nil {
		t.Fatal("expected a non-zero exit error")
	}
}

func TestInteractive_NotFound(t *testing.T) {
	err := Interactive("definitely-not-a-binary-xyz")
	if err == nil {
		t.Fatal("expected an error for a missing binary")
	}
}

func TestCombinedOutput_CapturesStderr(t *testing.T) {
	skipIfNoShellCmds(t)
	// Write to stderr via sh; CombinedOutput must capture it.
	out, err := CombinedOutput(5*time.Second, "sh", "-c", "echo oops 1>&2; exit 1")
	if err == nil {
		t.Fatal("expected a non-zero exit error")
	}
	if !strings.Contains(string(out), "oops") {
		t.Errorf("combined output = %q, want it to include stderr 'oops'", out)
	}
}
