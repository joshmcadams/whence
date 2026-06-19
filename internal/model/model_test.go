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
