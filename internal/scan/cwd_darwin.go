//go:build darwin

package scan

import (
	"fmt"
	"strings"
	"time"

	"github.com/joshmcadams/whence/internal/execx"
)

// lsofBatchTimeout bounds one batch lsof call over all process pids.
const lsofBatchTimeout = 5 * time.Second

// processCwds resolves working directories for every given pid with a
// single lsof call. lsof accepts comma-separated pids and -Fpn tags each
// n line with its p pid, so one invocation replaces N sequential calls.
//
// On error, stdout is still parsed (same partial-tolerance pattern: lsof
// exits non-zero when ANY listed pid has no cwd fd, but the rest may
// succeed). Only completely empty output is treated as a failure, recorded
// as a note on every attributed row — never a scan abort.
func processCwds(pids []int32) map[int32]cwdResult {
	if len(pids) == 0 {
		return nil
	}
	pidStrs := make([]string, len(pids))
	for i, pid := range pids {
		pidStrs[i] = fmt.Sprint(pid)
	}
	pidList := strings.Join(pidStrs, ",")

	out, err := execx.Output(lsofBatchTimeout, "lsof", "-a", "-p", pidList, "-d", "cwd", "-Fpn")

	result := make(map[int32]cwdResult, len(pids))
	parsed := parseLsofCwds(out)

	for pid, path := range parsed {
		result[pid] = cwdResult{path: path}
	}

	if err != nil && len(parsed) == 0 {
		batchErr := fmt.Errorf("lsof: %w", err)
		for _, pid := range pids {
			if _, seen := result[pid]; !seen {
				result[pid] = cwdResult{err: batchErr}
			}
		}
	} else {
		for _, pid := range pids {
			if _, seen := result[pid]; !seen {
				result[pid] = cwdResult{err: fmt.Errorf("not reported by lsof")}
			}
		}
	}

	return result
}
