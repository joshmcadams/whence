package kill

import (
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/joshmcadams/whence/internal/model"
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

func TestClimbCycle_DoesNotHang(t *testing.T) {
	// A ppid cycle between two launcher-named pids: 100 -> 200 -> 100. Both
	// named "npm" so the launcher check alone would loop forever without a
	// visited-set guard.
	tbl := table(
		map[int]int{100: 200, 200: 100},
		map[int]string{100: "npm", 200: "npm"},
	)
	done := make(chan int, 1)
	go func() { done <- climb(100, tbl) }()
	select {
	case got := <-done:
		if got != 100 && got != 200 {
			t.Errorf("climb = %d, want one of the cycle members (100 or 200)", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("climb did not return within 5s — cycle guard missing")
	}
}

func TestSubtreeCycle_DoesNotHangAndDedupes(t *testing.T) {
	// A child-link cycle: 100 <-> 200 (each lists the other as a child).
	tbl := procTable{
		ppid:     map[int]int{},
		name:     map[int]string{100: "npm", 200: "npm"},
		children: map[int][]int{100: {200}, 200: {100}},
	}
	done := make(chan []int, 1)
	go func() { done <- subtree(100, tbl) }()
	select {
	case got := <-done:
		sort.Ints(got)
		want := []int{100, 200}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("subtree = %v, want %v (each pid exactly once)", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("subtree did not return within 5s — cycle guard missing")
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

func TestPreviewWith_ClimbsTree(t *testing.T) {
	// bash(100) -> npm(200) -> node(300, listener). previewWith should climb to
	// npm (200) and include node (300) in the tree.
	tbl := table(
		map[int]int{200: 100, 300: 200, 100: 1},
		map[int]string{100: "bash", 200: "npm", 300: "node"},
	)
	s := model.Server{Source: model.SourceProcess, PID: 300, Port: 3000}
	plan := previewWith(s, Opts{}, tbl)
	if len(plan.Tree) == 0 {
		t.Fatal("expected non-empty tree")
	}
	pids := make([]int, len(plan.Tree))
	for i, m := range plan.Tree {
		pids[i] = m.PID
	}
	sort.Ints(pids)
	want := []int{200, 300}
	if !reflect.DeepEqual(pids, want) {
		t.Errorf("tree pids = %v, want %v", pids, want)
	}
}

// --- execution-path characterization tests ----------------------------------
//
// saveKillSeams snapshots the package-level seam vars and restores them on
// cleanup, so each test can freely reassign takeSnapshot/terminatePID/
// forceKillPID/pidAlive/dockerCombinedOutput without leaking into other tests.
func saveKillSeams(t *testing.T) {
	t.Helper()
	origSnap, origTerm, origForce, origAlive, origDocker :=
		takeSnapshot, terminatePID, forceKillPID, pidAlive, dockerCombinedOutput
	t.Cleanup(func() {
		takeSnapshot = origSnap
		terminatePID = origTerm
		forceKillPID = origForce
		pidAlive = origAlive
		dockerCombinedOutput = origDocker
	})
}

// launcherTree returns a synthetic tree: bash(1) -> make(2) -> {node(3, the
// listener), other(4)}. climb(3) reaches make(2); the full tree is {2,3,4}.
func launcherTree() procTable {
	return table(
		map[int]int{2: 1, 3: 2, 4: 2},
		map[int]string{1: "bash", 2: "make", 3: "node", 4: "node"},
	)
}

func TestServer_GracefulSuccess(t *testing.T) {
	saveKillSeams(t)
	tbl := launcherTree()
	takeSnapshot = func() procTable { return tbl }

	var mu sync.Mutex
	terminated := map[int]int{}
	terminatePID = func(pid int) error {
		mu.Lock()
		terminated[pid]++
		mu.Unlock()
		return nil
	}
	pidAlive = func(pid int) bool { return false } // dead as soon as checked
	forceCalled := false
	forceKillPID = func(pid int) error {
		forceCalled = true
		return nil
	}

	res := Server(model.Server{Source: model.SourceProcess, PID: 3}, Opts{Timeout: 300 * time.Millisecond})
	if !res.Killed || res.Err != nil {
		t.Fatalf("Killed=%v Err=%v, want Killed=true Err=nil", res.Killed, res.Err)
	}
	if res.Method != "tree" {
		t.Errorf("Method = %q, want %q", res.Method, "tree")
	}
	want := []int{2, 3, 4}
	for _, p := range want {
		if terminated[p] != 1 {
			t.Errorf("pid %d terminated %d time(s), want exactly 1", p, terminated[p])
		}
	}
	if len(terminated) != len(want) {
		t.Errorf("terminated %v, want exactly %v", terminated, want)
	}
	if forceCalled {
		t.Error("forceKillPID must not be called when SIGTERM already succeeded")
	}
}

func TestServer_EscalatesToForceKillAfterDeadline(t *testing.T) {
	saveKillSeams(t)
	tbl := launcherTree()
	takeSnapshot = func() procTable { return tbl }

	var mu sync.Mutex
	alive := map[int]bool{2: true, 3: true, 4: true}
	forceCallCount := map[int]int{}
	terminatePID = func(pid int) error { return nil }
	pidAlive = func(pid int) bool {
		mu.Lock()
		defer mu.Unlock()
		return alive[pid]
	}
	forceKillPID = func(pid int) error {
		mu.Lock()
		forceCallCount[pid]++
		alive[pid] = false
		mu.Unlock()
		return nil
	}

	timeout := 300 * time.Millisecond
	start := time.Now()
	res := Server(model.Server{Source: model.SourceProcess, PID: 3}, Opts{Timeout: timeout})
	elapsed := time.Since(start)

	if !res.Killed || res.Err != nil {
		t.Fatalf("Killed=%v Err=%v, want Killed=true Err=nil after escalation", res.Killed, res.Err)
	}
	if elapsed < timeout {
		t.Errorf("elapsed = %s, want >= timeout %s (force-kill must wait for the deadline)", elapsed, timeout)
	}
	for _, p := range []int{2, 3, 4} {
		if forceCallCount[p] != 1 {
			t.Errorf("pid %d force-killed %d time(s), want exactly 1", p, forceCallCount[p])
		}
	}
}

func TestServer_SurvivorAfterForceKillIsError(t *testing.T) {
	saveKillSeams(t)
	tbl := launcherTree()
	takeSnapshot = func() procTable { return tbl }

	terminatePID = func(pid int) error { return nil }
	pidAlive = func(pid int) bool { return true } // never dies, even after force-kill
	forceKillPID = func(pid int) error { return nil }

	res := Server(model.Server{Source: model.SourceProcess, PID: 3}, Opts{Timeout: 300 * time.Millisecond})
	if res.Killed {
		t.Error("Killed = true, want false when processes survive SIGKILL")
	}
	if res.Err == nil || res.Err.Error() != "processes survived SIGKILL" {
		t.Errorf("Err = %v, want %q", res.Err, "processes survived SIGKILL")
	}
}

func TestServer_SingleSignalsOnlyTheListener(t *testing.T) {
	saveKillSeams(t)
	tbl := launcherTree()
	takeSnapshot = func() procTable { return tbl }

	var mu sync.Mutex
	terminated := map[int]int{}
	terminatePID = func(pid int) error {
		mu.Lock()
		terminated[pid]++
		mu.Unlock()
		return nil
	}
	pidAlive = func(pid int) bool { return false }
	forceKillPID = func(pid int) error { return nil }

	res := Server(model.Server{Source: model.SourceProcess, PID: 3}, Opts{Single: true, Timeout: 300 * time.Millisecond})
	if !res.Killed || res.Err != nil {
		t.Fatalf("Killed=%v Err=%v, want success", res.Killed, res.Err)
	}
	if res.Method != "single" {
		t.Errorf("Method = %q, want %q", res.Method, "single")
	}
	if len(terminated) != 1 || terminated[3] != 1 {
		t.Errorf("terminated = %v, want only pid 3 signaled once (launcher parent and sibling untouched)", terminated)
	}
}

func TestServer_ZeroTimeoutStillReturnsPromptlyOnSuccess(t *testing.T) {
	saveKillSeams(t)
	tbl := launcherTree()
	takeSnapshot = func() procTable { return tbl }

	terminatePID = func(pid int) error { return nil }
	pidAlive = func(pid int) bool { return false } // dead immediately
	forceKillPID = func(pid int) error { return nil }

	start := time.Now()
	res := Server(model.Server{Source: model.SourceProcess, PID: 3}, Opts{}) // Timeout: 0 -> defaults to 5s internally
	elapsed := time.Since(start)

	if !res.Killed || res.Err != nil {
		t.Fatalf("Killed=%v Err=%v, want success", res.Killed, res.Err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("elapsed = %s, want a prompt return (zero timeout must not mean a hang or crash)", elapsed)
	}
}

func TestServer_NoPIDReturnsError(t *testing.T) {
	res := Server(model.Server{Source: model.SourceProcess, PID: 0}, Opts{})
	if res.Killed {
		t.Error("Killed = true, want false with no accessible pid")
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "no accessible pid") {
		t.Errorf("Err = %v, want it to contain %q", res.Err, "no accessible pid")
	}
}

// --- dockerStop characterization tests ---------------------------------------

type dockerCall struct {
	timeout time.Duration
	name    string
	args    []string
}

func TestDockerStop_SuccessUsesTimeoutAndArgs(t *testing.T) {
	saveKillSeams(t)
	var got dockerCall
	dockerCombinedOutput = func(timeout time.Duration, name string, args ...string) ([]byte, error) {
		got = dockerCall{timeout: timeout, name: name, args: args}
		return []byte("ok"), nil
	}

	res := Server(model.Server{Source: model.SourceDocker, Name: "web-1"}, Opts{Timeout: 7 * time.Second})
	if res.Err != nil || !res.Killed {
		t.Fatalf("Killed=%v Err=%v, want success", res.Killed, res.Err)
	}
	if res.Method != "docker stop" {
		t.Errorf("Method = %q, want %q", res.Method, "docker stop")
	}
	if got.name != "docker" {
		t.Errorf("binary = %q, want docker", got.name)
	}
	wantArgs := []string{"stop", "-t", "7", "web-1"}
	if !reflect.DeepEqual(got.args, wantArgs) {
		t.Errorf("args = %v, want %v", got.args, wantArgs)
	}
	if got.timeout != 17*time.Second {
		t.Errorf("call timeout = %s, want %s (7s stop + 10s slack)", got.timeout, 17*time.Second)
	}
}

func TestDockerStop_ZeroTimeoutClampsToFive(t *testing.T) {
	saveKillSeams(t)
	var got dockerCall
	dockerCombinedOutput = func(timeout time.Duration, name string, args ...string) ([]byte, error) {
		got = dockerCall{timeout: timeout, name: name, args: args}
		return nil, nil
	}

	res := Server(model.Server{Source: model.SourceDocker, Name: "web-1"}, Opts{})
	if res.Err != nil || !res.Killed {
		t.Fatalf("Killed=%v Err=%v, want success", res.Killed, res.Err)
	}
	wantArgs := []string{"stop", "-t", "5", "web-1"}
	if !reflect.DeepEqual(got.args, wantArgs) {
		t.Errorf("args = %v, want %v", got.args, wantArgs)
	}
	if got.timeout != 15*time.Second {
		t.Errorf("call timeout = %s, want %s (5s default stop + 10s slack)", got.timeout, 15*time.Second)
	}
}

func TestDockerStop_FailurePropagatesOutputAndError(t *testing.T) {
	saveKillSeams(t)
	dockerCombinedOutput = func(timeout time.Duration, name string, args ...string) ([]byte, error) {
		return []byte("boom"), errors.New("exit 1")
	}

	res := Server(model.Server{Source: model.SourceDocker, Name: "web-1"}, Opts{})
	if res.Killed {
		t.Error("Killed = true, want false on docker stop failure")
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "exit 1") || !strings.Contains(res.Err.Error(), "boom") {
		t.Errorf("Err = %v, want it to contain both %q and %q", res.Err, "exit 1", "boom")
	}
}

func TestDockerStop_EmptyNameNeverCallsDocker(t *testing.T) {
	saveKillSeams(t)
	called := false
	dockerCombinedOutput = func(timeout time.Duration, name string, args ...string) ([]byte, error) {
		called = true
		return nil, nil
	}

	res := Server(model.Server{Source: model.SourceDocker, Name: ""}, Opts{})
	if res.Killed {
		t.Error("Killed = true, want false with no container name")
	}
	if res.Err == nil || res.Err.Error() != "no container name" {
		t.Errorf("Err = %v, want %q", res.Err, "no container name")
	}
	if called {
		t.Error("dockerCombinedOutput must not be called when there is no container name")
	}
}

// --- Preview / PreviewBatch / Lines / process-table primitives --------------
//
// These exercise the real (unfaked) snapshot/isAlive against the actual host
// process table, using the test binary's own pid as a guaranteed-live process.

func TestPreview_DockerSource(t *testing.T) {
	p := Preview(model.Server{Source: model.SourceDocker, Name: "x"}, Opts{})
	if !p.Docker {
		t.Error("want a Docker plan for a docker-source server")
	}
}

func TestPreview_NoPID(t *testing.T) {
	p := Preview(model.Server{Source: model.SourceProcess, PID: 0}, Opts{})
	if !p.NoPID {
		t.Error("want a NoPID plan when PID <= 0")
	}
}

func TestPreview_NativeUsesRealSnapshot(t *testing.T) {
	p := Preview(model.Server{Source: model.SourceProcess, PID: os.Getpid()}, Opts{Single: true})
	if p.Docker || p.NoPID {
		t.Fatalf("Docker=%v NoPID=%v, want neither", p.Docker, p.NoPID)
	}
	if len(p.Tree) != 1 || p.Tree[0].PID != os.Getpid() {
		t.Errorf("Tree = %+v, want a single entry for our own pid (Single=true)", p.Tree)
	}
}

func TestPreviewBatch_Mixed(t *testing.T) {
	servers := []model.Server{
		{Source: model.SourceDocker, Name: "c1"},
		{Source: model.SourceProcess, PID: 0},
		{Source: model.SourceProcess, PID: os.Getpid()},
	}
	plans := PreviewBatch(servers, Opts{Single: true})
	if len(plans) != 3 {
		t.Fatalf("got %d plans, want 3", len(plans))
	}
	if !plans[0].Docker {
		t.Error("plan 0 should be Docker")
	}
	if !plans[1].NoPID {
		t.Error("plan 1 should be NoPID")
	}
	if plans[2].Docker || plans[2].NoPID || len(plans[2].Tree) != 1 || plans[2].Tree[0].PID != os.Getpid() {
		t.Errorf("plan 2 = %+v, want a native single-entry tree for our own pid", plans[2])
	}
}

func TestPlan_Lines(t *testing.T) {
	docker := Plan{Docker: true}
	if got := docker.Lines(); len(got) != 1 || got[0] != "docker stop" {
		t.Errorf("docker lines = %v, want [%q]", got, "docker stop")
	}

	nopid := Plan{NoPID: true}
	if got := nopid.Lines(); len(got) != 1 || !strings.Contains(got[0], "no accessible pid") {
		t.Errorf("nopid lines = %v, want it to mention 'no accessible pid'", got)
	}

	tree := Plan{Tree: []TreeMember{{PID: 1, Name: "make"}, {PID: 2, Name: ""}}}
	got := tree.Lines()
	if len(got) != 2 {
		t.Fatalf("got %d lines, want 2", len(got))
	}
	if got[0] != "1 make  (tree root)" {
		t.Errorf("line 0 = %q, want the tree-root tag (len(Tree) > 1)", got[0])
	}
	if got[1] != "2 ?" {
		t.Errorf("line 1 = %q, want the '?' placeholder for an empty name", got[1])
	}
}

func TestIsAlive(t *testing.T) {
	if !isAlive(os.Getpid()) {
		t.Error("isAlive(own pid) = false, want true")
	}
	if isAlive(999999999) {
		t.Error("isAlive(implausibly large pid) = true, want false")
	}
}

func TestSnapshot_IncludesCurrentProcess(t *testing.T) {
	tbl := snapshot()
	if _, ok := tbl.ppid[os.Getpid()]; !ok {
		t.Errorf("snapshot missing our own pid %d in the live process table", os.Getpid())
	}
}
