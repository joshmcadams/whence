package kill

import (
	"reflect"
	"sort"
	"testing"
)

// table builds a procTable from parent/name maps for testing.
func table(ppid map[int]int, name map[int]string) procTable {
	t := procTable{ppid: ppid, name: name, children: map[int][]int{}}
	for pid, pp := range ppid {
		t.children[pp] = append(t.children[pp], pid)
	}
	return t
}

func TestClimb_ThroughLaunchersStopsAtShell(t *testing.T) {
	// bash(100) -> npm(200) -> node(300) -> esbuild(400, the listener)
	tbl := table(
		map[int]int{200: 100, 300: 200, 400: 300, 100: 1},
		map[int]string{100: "bash", 200: "npm", 300: "node", 400: "esbuild"},
	)
	if got := climb(400, tbl); got != 200 {
		t.Errorf("climb = %d, want 200 (npm) — must climb node+npm but stop at bash", got)
	}
}

func TestClimb_DirectShellParentDoesNotClimb(t *testing.T) {
	// bash(100) -> python(200, listener run directly). Must not kill bash.
	tbl := table(
		map[int]int{200: 100, 100: 1},
		map[int]string{100: "bash", 200: "python3"},
	)
	if got := climb(200, tbl); got != 200 {
		t.Errorf("climb = %d, want 200 (do not climb into the shell)", got)
	}
}

func TestSubtree(t *testing.T) {
	tbl := table(
		map[int]int{200: 100, 300: 200, 400: 300, 500: 200},
		map[int]string{},
	)
	got := subtree(200, tbl)
	sort.Ints(got)
	want := []int{200, 300, 400, 500}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("subtree = %v, want %v", got, want)
	}
}
