package scan

import (
	"testing"

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
