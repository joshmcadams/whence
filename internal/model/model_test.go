package model

import "testing"

func TestDisplayName(t *testing.T) {
	cases := []struct {
		name string
		srv  Server
		want string
	}{
		{"prefers project name", Server{Name: "node", Project: &Project{Name: "myapp"}}, "myapp"},
		{"falls back to process name", Server{Name: "node"}, "node"},
		{"empty project name falls back", Server{Name: "node", Project: &Project{Name: ""}}, "node"},
		{"nothing known", Server{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.srv.DisplayName(); got != tc.want {
				t.Errorf("DisplayName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestDescription(t *testing.T) {
	if got := (Server{Project: &Project{Description: "d"}}).Description(); got != "d" {
		t.Errorf("Description() = %q, want d", got)
	}
	if got := (Server{}).Description(); got != "" {
		t.Errorf("Description() with no project = %q, want empty", got)
	}
}

func TestExposure(t *testing.T) {
	cases := []struct {
		addr string
		want string
	}{
		{"127.0.0.1", "local"},
		{"::1", "local"},
		{"localhost", "local"},
		{"0.0.0.0", "all"},
		{"::", "all"},
		{"", "all"},
		{"*", "all"}, // lsof/darwin wildcard bind
		{"192.168.1.5", "192.168.1.5"},
		{"10.0.0.1", "10.0.0.1"},
	}
	for _, tc := range cases {
		s := Server{Address: tc.addr}
		if got := s.Exposure(); got != tc.want {
			t.Errorf("Exposure(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}

func TestIsAllInterfaces(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"", true},
		{"0.0.0.0", true},
		{"::", true},
		{"*", true},
		{"127.0.0.1", false},
		{"::1", false},
		{"localhost", false},
		{"192.168.1.5", false},
	}
	for _, tc := range cases {
		if got := IsAllInterfaces(tc.addr); got != tc.want {
			t.Errorf("IsAllInterfaces(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestIsLoopback(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"localhost", true},
		{"", false},
		{"0.0.0.0", false},
		{"::", false},
		{"*", false},
		{"192.168.1.5", false},
	}
	for _, tc := range cases {
		if got := IsLoopback(tc.addr); got != tc.want {
			t.Errorf("IsLoopback(%q) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

func TestAttributed(t *testing.T) {
	if !(Server{PID: 42}).Attributed() {
		t.Error("PID 42 should be attributed")
	}
	if (Server{PID: 0}).Attributed() {
		t.Error("PID 0 (no owning pid) should not be attributed")
	}
	if (Server{PID: -1}).Attributed() {
		t.Error("negative PID should not be attributed")
	}
}
