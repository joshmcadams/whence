package scan

import (
	"testing"
	"time"

	gnet "github.com/shirou/gopsutil/v4/net"

	"github.com/joshmcadams/whence/internal/model"
)

func TestCollapseIPv4IPv6_AllInterfaces(t *testing.T) {
	in := []model.Server{
		{Port: 3000, Proto: "tcp", Address: "0.0.0.0", PID: 42, Source: model.SourceProcess},
		{Port: 3000, Proto: "tcp6", Address: "::", PID: 42, Source: model.SourceProcess},
	}
	got := collapseIPv4IPv6(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 server after collapse, got %d", len(got))
	}
	if got[0].Proto != "tcp" || got[0].Address != "0.0.0.0" {
		t.Errorf("collapsed entry: proto=%q address=%q, want tcp/0.0.0.0", got[0].Proto, got[0].Address)
	}
}

func TestCollapseIPv4IPv6_Loopback(t *testing.T) {
	in := []model.Server{
		{Port: 5432, Proto: "tcp", Address: "127.0.0.1", PID: 99, Source: model.SourceProcess},
		{Port: 5432, Proto: "tcp6", Address: "::1", PID: 99, Source: model.SourceProcess},
	}
	got := collapseIPv4IPv6(in)
	if len(got) != 1 {
		t.Fatalf("expected 1 server after collapse, got %d", len(got))
	}
	if got[0].Proto != "tcp" || got[0].Address != "127.0.0.1" {
		t.Errorf("collapsed entry: proto=%q address=%q, want tcp/127.0.0.1", got[0].Proto, got[0].Address)
	}
}

func TestCollapseIPv4IPv6_DifferentExposure(t *testing.T) {
	// all-interfaces + loopback on the same port/pid should NOT be collapsed.
	in := []model.Server{
		{Port: 8080, Proto: "tcp", Address: "0.0.0.0", PID: 10, Source: model.SourceProcess},
		{Port: 8080, Proto: "tcp6", Address: "::1", PID: 10, Source: model.SourceProcess},
	}
	got := collapseIPv4IPv6(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 servers (different exposure, no collapse), got %d", len(got))
	}
}

func TestCollapseIPv4IPv6_NoPID(t *testing.T) {
	// Unattributed entries (PID=0) are never collapsed.
	in := []model.Server{
		{Port: 9999, Proto: "tcp", Address: "0.0.0.0", PID: 0, Source: model.SourceProcess},
		{Port: 9999, Proto: "tcp6", Address: "::", PID: 0, Source: model.SourceProcess},
	}
	got := collapseIPv4IPv6(in)
	if len(got) != 2 {
		t.Fatalf("expected 2 servers (no pid = no collapse), got %d", len(got))
	}
}

func TestRowsFromConns_DropsNonListen(t *testing.T) {
	conns := []gnet.ConnectionStat{
		{Status: "ESTABLISHED", Family: 2, Laddr: gnet.Addr{IP: "10.0.0.1", Port: 443}, Pid: 100},
		{Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "127.0.0.1", Port: 8080}, Pid: 100},
	}
	noop := func(*model.Server, int32, time.Time) {}
	got := rowsFromConns(conns, time.Now(), noop)
	if len(got) != 1 {
		t.Fatalf("expected 1 row (non-LISTEN dropped), got %d", len(got))
	}
	if got[0].Port != 8080 {
		t.Errorf("expected surviving row to be port 8080, got %d", got[0].Port)
	}
}

func TestRowsFromConns_DedupsExactDuplicates(t *testing.T) {
	conns := []gnet.ConnectionStat{
		{Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "127.0.0.1", Port: 8080}, Pid: 100},
		{Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "127.0.0.1", Port: 8080}, Pid: 100},
	}
	noop := func(*model.Server, int32, time.Time) {}
	got := rowsFromConns(conns, time.Now(), noop)
	if len(got) != 1 {
		t.Fatalf("expected exact duplicates to dedup to 1 row, got %d", len(got))
	}
}

func TestRowsFromConns_DistinctUnattributedListenersSurvive(t *testing.T) {
	// Two different unattributed (Pid=0) processes on the same port/proto but
	// different bind addresses must both survive — this is the fix. Against
	// the old (port, proto, pid) key, these would incorrectly collapse to 1.
	conns := []gnet.ConnectionStat{
		{Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "127.0.0.53", Port: 53}, Pid: 0},
		{Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "192.168.122.1", Port: 53}, Pid: 0},
	}
	noop := func(*model.Server, int32, time.Time) {}
	got := rowsFromConns(conns, time.Now(), noop)
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct unattributed listeners on port 53, got %d", len(got))
	}
}

func TestRowsFromConns_NoPIDGetsNoteAndSkipsEnrich(t *testing.T) {
	conns := []gnet.ConnectionStat{
		{Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "0.0.0.0", Port: 53}, Pid: 0},
	}
	called := false
	enrichFn := func(*model.Server, int32, time.Time) { called = true }
	got := rowsFromConns(conns, time.Now(), enrichFn)
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if called {
		t.Error("enrichFn must not be called for a PID=0 row")
	}
	found := false
	for _, n := range got[0].Notes {
		if n == "no pid (owned by another user; rerun with elevated privileges)" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected no-pid note, got notes=%v", got[0].Notes)
	}
}

func TestRowsFromConns_PIDCallsEnrichOnce(t *testing.T) {
	conns := []gnet.ConnectionStat{
		{Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "127.0.0.1", Port: 8080}, Pid: 42},
	}
	var callCount int
	var gotPid int32
	enrichFn := func(_ *model.Server, pid int32, _ time.Time) {
		callCount++
		gotPid = pid
	}
	got := rowsFromConns(conns, time.Now(), enrichFn)
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if callCount != 1 {
		t.Errorf("expected enrichFn called exactly once, got %d", callCount)
	}
	if gotPid != 42 {
		t.Errorf("expected enrichFn called with pid 42, got %d", gotPid)
	}
}

func TestRowsFromConns_DualStackSamePID_CollapsesViaCollapseIPv4IPv6(t *testing.T) {
	conns := []gnet.ConnectionStat{
		{Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "0.0.0.0", Port: 3000}, Pid: 42},
		{Status: "LISTEN", Family: 10, Laddr: gnet.Addr{IP: "::", Port: 3000}, Pid: 42},
	}
	noop := func(*model.Server, int32, time.Time) {}
	rows := rowsFromConns(conns, time.Now(), noop)
	if len(rows) != 2 {
		t.Fatalf("expected rowsFromConns to keep both dual-stack rows (collapse happens downstream), got %d", len(rows))
	}

	collapsed := collapseIPv4IPv6(rows)
	if len(collapsed) != 1 {
		t.Fatalf("expected collapseIPv4IPv6 to merge the dual-stack pair into 1 row, got %d", len(collapsed))
	}
	if collapsed[0].Proto != "tcp" || collapsed[0].Address != "0.0.0.0" {
		t.Errorf("collapsed entry: proto=%q address=%q, want tcp/0.0.0.0", collapsed[0].Proto, collapsed[0].Address)
	}
}

func TestRowsFromConns_WildcardAddressExposureAll(t *testing.T) {
	conns := []gnet.ConnectionStat{
		{Status: "LISTEN", Family: 2, Laddr: gnet.Addr{IP: "*", Port: 8080}, Pid: 42},
	}
	noop := func(*model.Server, int32, time.Time) {}
	got := rowsFromConns(conns, time.Now(), noop)
	if len(got) != 1 {
		t.Fatalf("expected 1 row, got %d", len(got))
	}
	if got[0].Exposure() != "all" {
		t.Errorf("Exposure() for wildcard address %q = %q, want %q", got[0].Address, got[0].Exposure(), "all")
	}
}

func TestProtoOf(t *testing.T) {
	cases := []struct {
		family uint32
		want   string
	}{
		{2, "tcp"},   // AF_INET
		{10, "tcp6"}, // AF_INET6 on linux
		{23, "tcp6"}, // AF_INET6 on windows
		{30, "tcp6"}, // AF_INET6 on darwin
		{0, "tcp"},   // unknown families default to tcp
		{99, "tcp"},
	}
	for _, tc := range cases {
		if got := protoOf(tc.family); got != tc.want {
			t.Errorf("protoOf(%d) = %q, want %q", tc.family, got, tc.want)
		}
	}
}
