package inventory

import (
	"testing"

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
