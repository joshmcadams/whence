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

func TestClimb_ThroughNpmStopsAtShell(t *testing.T) {
	// bash(100) -> npm(200) -> node(300, the listener). Climb to npm, stop at bash.
	tbl := table(
		map[int]int{200: 100, 300: 200, 100: 1},
		map[int]string{100: "bash", 200: "npm", 300: "node"},
	)
	if got := climb(300, tbl); got != 200 {
		t.Errorf("climb = %d, want 200 (npm) — climb through npm but stop at bash", got)
	}
}

func TestClimb_DoesNotClimbThroughBareNode(t *testing.T) {
	// node(200, e.g. an editor host) -> node(300, the listener). We must NOT
	// climb into the parent node and risk killing the host.
	tbl := table(
		map[int]int{200: 1, 300: 200},
		map[int]string{200: "node", 300: "node"},
	)
	if got := climb(300, tbl); got != 300 {
		t.Errorf("climb = %d, want 300 (must not climb through a bare node parent)", got)
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

func TestPlanTree_SingleIsJustTheListener(t *testing.T) {
	// bash(100) -> npm(200) -> node(300). --single must signal only 300.
	tbl := table(
		map[int]int{200: 100, 300: 200, 100: 1},
		map[int]string{100: "bash", 200: "npm", 300: "node"},
	)
	if got := planTree(300, true, tbl); !reflect.DeepEqual(got, []int{300}) {
		t.Errorf("planTree single = %v, want [300]", got)
	}
}

func TestPlanTree_ClimbsAndIncludesSiblings(t *testing.T) {
	// bash(100) -> make(200) -> {node(300, our listener), other(400)}.
	// Killing 300's tree climbs to make and takes the sibling 400 too — the
	// blast radius the confirmation must reveal.
	tbl := table(
		map[int]int{200: 100, 300: 200, 400: 200, 100: 1},
		map[int]string{100: "bash", 200: "make", 300: "node", 400: "node"},
	)
	got := planTree(300, false, tbl)
	sort.Ints(got)
	want := []int{200, 300, 400}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("planTree = %v, want %v (climbs to make, includes sibling)", got, want)
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
