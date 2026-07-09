package inventory

import (
	"strconv"
	"testing"
	"time"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/model"
)

func servers() []model.Server {
	return []model.Server{
		{Port: 5173, Confidence: 100, Project: &model.Project{Name: "jfdid", Description: "task system"}},
		{Port: 8080, Confidence: 80, Project: &model.Project{Name: "jfdid"}},
		{Port: 9999, Confidence: 0, Name: "sshd"},
	}
}

func TestView_ConfidenceFilter(t *testing.T) {
	cfg := config.Config{ConfidenceThreshold: 50}
	mine := View(servers(), cfg, false, 0, "")
	if len(mine) != 2 {
		t.Fatalf("mine = %d, want 2", len(mine))
	}
	all := View(servers(), cfg, true, 0, "")
	if len(all) != 3 {
		t.Fatalf("all = %d, want 3", len(all))
	}
}

func TestView_PortAndQuery(t *testing.T) {
	cfg := config.Config{ConfidenceThreshold: 50}
	if got := View(servers(), cfg, true, 8080, ""); len(got) != 1 || got[0].Port != 8080 {
		t.Fatalf("port filter = %v", got)
	}
	if got := View(servers(), cfg, false, 0, "task"); len(got) != 1 || got[0].Port != 5173 {
		t.Fatalf("query 'task' = %v", got)
	}
	// query matches port number text
	if got := View(servers(), cfg, true, 0, "999"); len(got) != 1 || got[0].Port != 9999 {
		t.Fatalf("query '999' = %v", got)
	}
}

func TestView_IgnorePorts(t *testing.T) {
	cfg := config.Config{ConfidenceThreshold: 50, IgnorePorts: []int{8080}}

	// Ignored even with all=true (where noise normally surfaces).
	got := View(servers(), cfg, true, 0, "")
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (8080 ignored)", len(got))
	}
	for _, s := range got {
		if s.Port == 8080 {
			t.Fatal("ignored port 8080 appeared even with --all")
		}
	}

	// An explicit --port is a direct request and overrides the ignore list.
	if got := View(servers(), cfg, true, 8080, ""); len(got) != 1 || got[0].Port != 8080 {
		t.Fatalf("explicit --port 8080 should override ignore, got %v", got)
	}
}

func TestMerge_ClassicProxy(t *testing.T) {
	// Root-owned docker-proxy listener (unattributed, PID<=0) mirrors the
	// container's published port — suppressed.
	dockers := []model.Server{{Port: 5433, Address: "0.0.0.0", Source: model.SourceDocker}}
	procs := []model.Server{{Port: 5433, PID: 0, Address: "0.0.0.0"}}
	got := merge(dockers, procs)
	if len(got) != 1 || got[0].Source != model.SourceDocker {
		t.Fatalf("merge = %+v, want only the docker row", got)
	}
}

func TestMerge_PrivilegedProxyByName(t *testing.T) {
	// Privileged scan attributes the proxy: PID>0 but name is docker-proxy —
	// still suppressed.
	dockers := []model.Server{{Port: 5433, Address: "0.0.0.0", Source: model.SourceDocker}}
	procs := []model.Server{{Port: 5433, PID: 900, Name: "docker-proxy", Address: "0.0.0.0"}}
	got := merge(dockers, procs)
	if len(got) != 1 || got[0].Source != model.SourceDocker {
		t.Fatalf("merge = %+v, want only the docker row", got)
	}
}

func TestMerge_DistinctInterfaceSurvives(t *testing.T) {
	// The fix: a container bound to loopback and a native listener on the
	// same port number but a genuinely different interface both survive.
	dockers := []model.Server{{Port: 8080, Address: "127.0.0.1", Source: model.SourceDocker}}
	procs := []model.Server{{Port: 8080, PID: 4242, Name: "python3", Address: "192.168.1.5"}}
	got := merge(dockers, procs)
	if len(got) != 2 {
		t.Fatalf("merge = %+v, want both rows to survive (fails against the old bare-port rule)", got)
	}
}

func TestMerge_AllInterfacesContainerSuppressesAttributedNative(t *testing.T) {
	// A container bound to all interfaces can't genuinely coexist with a
	// same-port, same-exposure native listener; treat the native row as the
	// proxy/mirror and suppress it even though it's attributed.
	dockers := []model.Server{{Port: 9090, Address: "0.0.0.0", Source: model.SourceDocker}}
	procs := []model.Server{{Port: 9090, PID: 555, Name: "someproc", Address: "0.0.0.0"}}
	got := merge(dockers, procs)
	if len(got) != 1 || got[0].Source != model.SourceDocker {
		t.Fatalf("merge = %+v, want only the docker row", got)
	}
}

func TestMerge_DisjointPortsPassThrough(t *testing.T) {
	dockers := []model.Server{{Port: 5432, Address: "0.0.0.0", Source: model.SourceDocker}}
	procs := []model.Server{{Port: 3000, PID: 111, Name: "node", Address: "127.0.0.1"}}
	got := merge(dockers, procs)
	if len(got) != 2 {
		t.Fatalf("merge = %+v, want both rows (disjoint ports)", got)
	}
	if got[0].Source != model.SourceDocker || got[1].Port != 3000 {
		t.Fatalf("merge order = %+v, want dockers first then procs", got)
	}
}

func TestMerge_EmptyDockerSetPassesProcsThrough(t *testing.T) {
	procs := []model.Server{
		{Port: 3000, PID: 111, Name: "node"},
		{Port: 4000, PID: 222, Name: "python3"},
	}
	got := merge(nil, procs)
	if len(got) != 2 {
		t.Fatalf("merge = %+v, want procs untouched", got)
	}
}

func TestView_IgnoreNames(t *testing.T) {
	// Case-insensitive match on the process name.
	cfg := config.Config{ConfidenceThreshold: 50, IgnoreNames: []string{"SSHD"}}
	got := View(servers(), cfg, true, 0, "")
	if len(got) != 2 {
		t.Fatalf("got %d, want 2 (sshd ignored)", len(got))
	}
	for _, s := range got {
		if s.Name == "sshd" {
			t.Fatal("ignored name sshd appeared")
		}
	}

	// Matches the project's display name too, so ignoring "jfdid" hides both
	// jfdid servers and leaves only sshd.
	cfg2 := config.Config{ConfidenceThreshold: 50, IgnoreNames: []string{"jfdid"}}
	if got := View(servers(), cfg2, true, 0, ""); len(got) != 1 || got[0].Name != "sshd" {
		t.Fatalf("ignoring 'jfdid' should leave only sshd, got %v", got)
	}
}

// key returns a tuple that uniquely identifies a server regardless of Sort's
// tiebreak chain, for asserting the two permutations below settled on the
// same row order (not just the same membership).
func key(s model.Server) [5]string {
	return [5]string{
		strconv.Itoa(s.Port), s.Proto, s.Address, strconv.Itoa(s.PID), s.Name,
	}
}

func keys(servers []model.Server) []([5]string) {
	out := make([][5]string, len(servers))
	for i, s := range servers {
		out[i] = key(s)
	}
	return out
}

// assertSameOrder fails unless two permutation runs of Sort produced the
// byte-identical row order.
func assertSameOrder(t *testing.T, by string, a, b []model.Server) {
	t.Helper()
	Sort(a, by)
	Sort(b, by)
	ka, kb := keys(a), keys(b)
	if len(ka) != len(kb) {
		t.Fatalf("Sort(%q): length mismatch %d vs %d", by, len(ka), len(kb))
	}
	for i := range ka {
		if ka[i] != kb[i] {
			t.Fatalf("Sort(%q) not permutation-stable at row %d: %v vs %v", by, i, ka, kb)
		}
	}
}

func TestSort_PortIsPermutationStable(t *testing.T) {
	// Two rows tie on port+proto+address+pid; only Name differs, so the full
	// defaultLess chain must be exercised to land on one deterministic order.
	orderA := []model.Server{
		{Port: 8080, Proto: "tcp", Address: "127.0.0.1", PID: 100, Name: "beta"},
		{Port: 8080, Proto: "tcp", Address: "127.0.0.1", PID: 100, Name: "alpha"},
		{Port: 3000, Proto: "tcp", Address: "0.0.0.0", PID: 1, Name: "z"},
	}
	orderB := []model.Server{
		{Port: 3000, Proto: "tcp", Address: "0.0.0.0", PID: 1, Name: "z"},
		{Port: 8080, Proto: "tcp", Address: "127.0.0.1", PID: 100, Name: "alpha"},
		{Port: 8080, Proto: "tcp", Address: "127.0.0.1", PID: 100, Name: "beta"},
	}
	assertSameOrder(t, "port", orderA, orderB)
}

func TestSort_UptimeIsPermutationStable(t *testing.T) {
	// Two rows share Uptime == 0 (the common "unknown uptime" case); the tie
	// must resolve via defaultLess, not input order.
	tie := 5 * time.Minute
	orderA := []model.Server{
		{Port: 9000, Uptime: 0, Name: "b"},
		{Port: 9000, Uptime: 0, Name: "a"},
		{Port: 4000, Uptime: tie, Name: "solo"},
	}
	orderB := []model.Server{
		{Port: 4000, Uptime: tie, Name: "solo"},
		{Port: 9000, Uptime: 0, Name: "a"},
		{Port: 9000, Uptime: 0, Name: "b"},
	}
	assertSameOrder(t, "uptime", orderA, orderB)
}

func TestSort_NameIsPermutationStable(t *testing.T) {
	// Two unattributed rows share an empty DisplayName; the tie must resolve
	// via defaultLess (port here), not input order.
	orderA := []model.Server{
		{Port: 9000, Name: ""},
		{Port: 4000, Name: ""},
		{Port: 1000, Project: &model.Project{Name: "zeta"}},
	}
	orderB := []model.Server{
		{Port: 1000, Project: &model.Project{Name: "zeta"}},
		{Port: 4000, Name: ""},
		{Port: 9000, Name: ""},
	}
	assertSameOrder(t, "name", orderA, orderB)
}
