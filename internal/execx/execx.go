// Package execx runs external commands with a hard timeout so a wedged
// dependency (a hung Docker daemon, a stuck lsof) can never hang whence.
//
// Every shell-out in the codebase goes through here; nothing should call
// os/exec.Command directly. On timeout the returned error says so, so callers
// (and the user, via doctor) can tell a hang from an ordinary failure.
package execx

import (
	"context"
	"fmt"
	"os/exec"
	"time"
)

// Output runs name+args with a hard timeout and returns stdout. If the timeout
// elapses the child is killed and a "timed out" error is returned.
func Output(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return out, wrap(ctx, name, timeout, err)
}

// CombinedOutput is Output but merges stderr into the result, for commands
// whose failure message we want to surface (e.g. `docker stop`).
func CombinedOutput(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return out, wrap(ctx, name, timeout, err)
}

// Run executes name+args with a hard timeout, discarding output.
func Run(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	err := exec.CommandContext(ctx, name, args...).Run()
	return wrap(ctx, name, timeout, err)
}

// wrap converts a deadline-exceeded context into a clear timeout error,
// otherwise returns the original command error unchanged.
func wrap(ctx context.Context, name string, timeout time.Duration, err error) error {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("%s timed out after %s", name, timeout)
	}
	return err
}
