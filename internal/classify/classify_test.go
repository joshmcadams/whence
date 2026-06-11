package classify

import (
	"testing"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/model"
)

func TestScoreProcess(t *testing.T) {
	cfg := config.Config{DevRoots: []string{"/home/me/dev"}, ConfidenceThreshold: 50}

	cases := []struct {
		name string
		srv  model.Server
		want int
	}{
		{
			name: "dev root + repo + dev cmd",
			srv:  model.Server{Cwd: "/home/me/dev/app", Project: &model.Project{Name: "app"}, Cmdline: "node .../vite"},
			want: 100,
		},
		{
			name: "dev root only",
			srv:  model.Server{Cwd: "/home/me/dev/scratch"},
			want: 50,
		},
		{
			name: "repo marker outside dev root",
			srv:  model.Server{Cwd: "/opt/thing", Project: &model.Project{Name: "thing"}},
			want: 30,
		},
		{
			name: "dev cmd only",
			srv:  model.Server{Cwd: "/tmp", Cmdline: "gunicorn app:app"},
			want: 20,
		},
		{
			name: "nothing",
			srv:  model.Server{Cwd: "/usr/sbin", Cmdline: "sshd"},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scoreProcess(tc.srv, cfg); got != tc.want {
				t.Errorf("score = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMineThreshold(t *testing.T) {
	cfg := config.Config{ConfidenceThreshold: 50}
	if !Mine(model.Server{Confidence: 50}, cfg) {
		t.Error("confidence 50 should clear threshold 50")
	}
	if Mine(model.Server{Confidence: 40}, cfg) {
		t.Error("confidence 40 should not clear threshold 50")
	}
}
