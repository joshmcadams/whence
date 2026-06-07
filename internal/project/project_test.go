package project

import (
	"os"
	"path/filepath"
	"testing"
)

// writeFile creates a file with content, making parent dirs as needed.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDetect_GitRootCollapsesMonorepoSubdir(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "package.json"),
		`{"name":"jfdid","description":"task system"}`)
	web := filepath.Join(root, "web")
	writeFile(t, filepath.Join(web, "package.json"), `{"name":"web"}`)

	// A process running from the web subdir must resolve to the repo root.
	got := Detect(web)
	if got == nil {
		t.Fatal("expected a project, got nil")
	}
	if got.Name != "jfdid" {
		t.Errorf("name = %q, want jfdid", got.Name)
	}
	if got.Root != root {
		t.Errorf("root = %q, want %q", got.Root, root)
	}
	if got.Marker != ".git" {
		t.Errorf("marker = %q, want .git", got.Marker)
	}
	if got.Description != "task system" {
		t.Errorf("description = %q, want 'task system'", got.Description)
	}
}

func TestDetect_ManifestFallbackWhenNoGit(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module github.com/me/widget\n\ngo 1.26\n")

	got := Detect(root)
	if got == nil {
		t.Fatal("expected a project, got nil")
	}
	if got.Name != "widget" { // last element of module path
		t.Errorf("name = %q, want widget", got.Name)
	}
	if got.Marker != "go.mod" {
		t.Errorf("marker = %q, want go.mod", got.Marker)
	}
}

func TestDetect_NoMarkersReturnsNil(t *testing.T) {
	if got := Detect(t.TempDir()); got != nil {
		t.Errorf("expected nil for a dir with no markers, got %+v", got)
	}
}

func TestReadmeSummary_SkipsHeadingsAndBadges(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "README.md"),
		"# Title\n\n![badge](x.png)\n\nThe first real sentence.\n")
	if got := readmeSummary(root); got != "The first real sentence." {
		t.Errorf("summary = %q", got)
	}
}

func TestName_FallsBackToDirBasename(t *testing.T) {
	root := filepath.Join(t.TempDir(), "myproj")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := name(root); got != "myproj" {
		t.Errorf("name = %q, want myproj", got)
	}
}
