package scan

import "testing"

func TestParseLsofCwds(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want map[int32]string
	}{
		{"single pid", "p123\nn/Users/x/dev/app\n",
			map[int32]string{123: "/Users/x/dev/app"}},
		{"batch", "p1\nn/a\np2\nn/b\n",
			map[int32]string{1: "/a", 2: "/b"}},
		{"missing cwd", "p1\np2\nn/b\n",
			map[int32]string{2: "/b"}},
		{"empty input", "",
			map[int32]string{}},
		{"spaces in path", "p1\nn/Users/x/my project\n",
			map[int32]string{1: "/Users/x/my project"}},
		{"garbage lines", "garbage\np1\nn/a\nf0\n",
			map[int32]string{1: "/a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseLsofCwds([]byte(tc.in))
			if len(got) != len(tc.want) {
				t.Errorf("len = %d, want %d; got=%v", len(got), len(tc.want), got)
				return
			}
			for pid, wantPath := range tc.want {
				if got[pid] != wantPath {
					t.Errorf("pid %d: got %q, want %q", pid, got[pid], wantPath)
				}
			}
		})
	}
}
