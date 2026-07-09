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

func TestDetect_GitFileWorktreeResolvesToWorktreeRoot(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ".git"), "gitdir: /somewhere/else\n")
	writeFile(t, filepath.Join(root, "web", "package.json"), `{"name":"web"}`)

	got := Detect(filepath.Join(root, "web"))
	if got == nil {
		t.Fatal("expected a project, got nil")
	}
	if got.Root != root {
		t.Errorf("root = %q, want %q", got.Root, root)
	}
	if got.Marker != ".git" {
		t.Errorf("marker = %q, want .git", got.Marker)
	}
}

func TestDetect_GitFileWorktreeDoesNotOverWalkToEnclosingRepo(t *testing.T) {
	outer := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outer, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	wt := filepath.Join(outer, "wt")
	writeFile(t, filepath.Join(wt, ".git"), "gitdir: /somewhere/else\n")
	src := filepath.Join(wt, "src")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}

	got := Detect(src)
	if got == nil {
		t.Fatal("expected a project, got nil")
	}
	if got.Root != wt {
		t.Errorf("root = %q, want %q (nearest marker, not enclosing repo %q)", got.Root, wt, outer)
	}
	if got.Marker != ".git" {
		t.Errorf("marker = %q, want .git", got.Marker)
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

func TestReadSmallFile_OversizedRejected(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "go.mod")
	big := make([]byte, maxManifestBytes+1)
	for i := range big {
		big[i] = 'a'
	}
	writeFile(t, path, string(big))

	if got := goModule(path); got != "" {
		t.Errorf("goModule on oversized file = %q, want empty", got)
	}
	if _, ok := readSmallFile(path); ok {
		t.Error("readSmallFile on oversized file: got ok=true, want false")
	}
}

func TestReadSmallFile_SymlinkToManifestRejected(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real-go.mod")
	writeFile(t, real, "module github.com/me/widget\n\ngo 1.26\n")
	link := filepath.Join(root, "go.mod")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	// Lstat rejects the symlink even though it points at a valid manifest —
	// an accepted behavior change (see readSmallFile's doc comment).
	if got := goModule(link); got != "" {
		t.Errorf("goModule via symlink = %q, want empty (symlinks rejected)", got)
	}
}

func TestCache_SameRootReturnsSamePointer(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	sub1 := filepath.Join(root, "backend")
	sub2 := filepath.Join(root, "frontend")
	if err := os.MkdirAll(sub1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub2, 0o755); err != nil {
		t.Fatal(err)
	}

	c := NewCache()
	p1 := c.Detect(sub1)
	p2 := c.Detect(sub2)

	if p1 == nil {
		t.Fatal("expected non-nil project for sub1")
	}
	if p1 != p2 {
		t.Error("same repo root: expected same *Project pointer (cache hit), got different allocations")
	}
}
