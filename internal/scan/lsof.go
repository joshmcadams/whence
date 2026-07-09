package scan

import (
	"strconv"
	"strings"
)

// parseLsofCwds parses `lsof -a -p <pids> -d cwd -Fpn` field output.
// lsof -F emits one field per line: 'p'-prefixed lines carry a pid,
// 'n'-prefixed lines carry the cwd path for the most recent pid.
// Returns pid → cwd for every (pid, path) pair found.
func parseLsofCwds(out []byte) map[int32]string {
	result := map[int32]string{}
	var curPid int32
	var hasPid bool
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) == 0 {
			continue
		}
		switch line[0] {
		case 'p':
			if pid, err := strconv.ParseInt(line[1:], 10, 32); err == nil {
				curPid = int32(pid)
				hasPid = true
			}
		case 'n':
			if hasPid {
				result[curPid] = line[1:]
			}
		}
	}
	return result
}
