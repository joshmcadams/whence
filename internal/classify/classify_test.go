package classify

import (
	"os"
	"path/filepath"
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

func TestProcess_DockerSkipped(t *testing.T) {
	cfg := config.Config{DevRoots: []string{}, ConfidenceThreshold: 50}
	servers := []model.Server{
		{Source: model.SourceDocker, Name: "web-1", Confidence: 80},
	}
	Process(servers, cfg)
	if servers[0].Confidence != 80 {
		t.Errorf("docker server confidence = %d, want 80 (untouched by Process)", servers[0].Confidence)
	}
	if servers[0].Project != nil {
		t.Errorf("docker server project = %+v, want nil", servers[0].Project)
	}
}

func TestProcess_EmptyCwdScoresWithoutProject(t *testing.T) {
	cfg := config.Config{DevRoots: []string{}, ConfidenceThreshold: 50}
	servers := []model.Server{
		{Source: model.SourceProcess, Cwd: "", Cmdline: "gunicorn app:app"},
	}
	Process(servers, cfg)
	// gunicorn matches devCmdHints, so score should be 20
	if servers[0].Confidence != 20 {
		t.Errorf("empty-cwd confidence = %d, want 20 (dev-cmd scored without project)", servers[0].Confidence)
	}
	if servers[0].Project != nil {
		t.Errorf("empty-cwd project = %+v, want nil (no cwd to resolve)", servers[0].Project)
	}
}

func TestProcess_NativeWithCwdGetsProjectAndScore(t *testing.T) {
	dir := t.TempDir()
	// Set up a fake project root: .git + package.json with a name.
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkg, []byte(`{"name": "test-app", "description": "a test project"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{DevRoots: []string{dir}, ConfidenceThreshold: 50}
	servers := []model.Server{
		{Source: model.SourceProcess, Cwd: dir, Cmdline: "node .../vite"},
	}
	Process(servers, cfg)
	if servers[0].Project == nil {
		t.Fatal("native server with cwd should get a project, got nil")
	}
	if servers[0].Project.Name != "test-app" {
		t.Errorf("project name = %q, want %q", servers[0].Project.Name, "test-app")
	}
	// dev root (50) + repo marker (30) + vite dev cmd (20) = 100, capped
	if servers[0].Confidence != 100 {
		t.Errorf("confidence = %d, want 100", servers[0].Confidence)
	}
}

func TestProcess_NativeOutsideDevRootStillGetsProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(dir, "package.json")
	if err := os.WriteFile(pkg, []byte(`{"name": "other-app"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Config{DevRoots: []string{}, ConfidenceThreshold: 50}
	servers := []model.Server{
		{Source: model.SourceProcess, Cwd: dir, Cmdline: "sshd"},
	}
	Process(servers, cfg)
	if servers[0].Project == nil {
		t.Fatal("cwd should resolve a project even outside dev roots")
	}
	if servers[0].Project.Name != "other-app" {
		t.Errorf("project name = %q, want %q", servers[0].Project.Name, "other-app")
	}
	// repo marker (30) + no dev root + no dev cmd = 30
	if servers[0].Confidence != 30 {
		t.Errorf("confidence = %d, want 30 (repo only, outside dev root, boring cmd)", servers[0].Confidence)
	}
}
