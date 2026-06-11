// Package project resolves the repository a process/container was launched from
// and extracts a display name and description.
package project

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/joshmcadams/whence/internal/model"
)

// manifests are non-.git markers that also indicate a project root, in priority
// order (used when no .git is found while walking up).
var manifests = []string{
	"go.mod", "package.json", "pyproject.toml", "Cargo.toml",
	"composer.json", "Gemfile", "build.gradle", "pom.xml", "Makefile",
}

// Detect walks up from startDir and returns the project it belongs to, or nil.
// The repo root is the nearest ancestor containing .git; failing that, the
// nearest ancestor containing a known manifest. Anchoring on .git means a
// monorepo's subdir (e.g. jfdid/web) resolves to the same project (jfdid) as
// the rest of the repo, so grouping/kill-by-name behaves intuitively.
func Detect(startDir string) *model.Project {
	if startDir == "" {
		return nil
	}
	root, marker := findRoot(startDir)
	if root == "" {
		return nil
	}
	return &model.Project{
		Name:        name(root),
		Root:        root,
		Description: Description(root),
		Marker:      marker,
	}
}

func findRoot(start string) (root, marker string) {
	dir := filepath.Clean(start)
	var firstManifest, firstManifestMarker string
	for {
		if isDir(filepath.Join(dir, ".git")) {
			return dir, ".git"
		}
		if firstManifest == "" {
			for _, m := range manifests {
				if exists(filepath.Join(dir, m)) {
					firstManifest, firstManifestMarker = dir, m
					break
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return firstManifest, firstManifestMarker
}

// name resolves the project name from a manifest, falling back to the dir name.
func name(root string) string {
	if n := jsonField(filepath.Join(root, "package.json"), "name"); n != "" {
		return n
	}
	if n := goModule(filepath.Join(root, "go.mod")); n != "" {
		return n
	}
	if n := tomlName(filepath.Join(root, "Cargo.toml")); n != "" {
		return n
	}
	if n := tomlName(filepath.Join(root, "pyproject.toml")); n != "" {
		return n
	}
	if n := jsonField(filepath.Join(root, "composer.json"), "name"); n != "" {
		return n
	}
	return filepath.Base(root)
}

// Description resolves a one-line description from a manifest, falling back to
// the first prose line of the README.
func Description(root string) string {
	if d := jsonField(filepath.Join(root, "package.json"), "description"); d != "" {
		return d
	}
	if d := tomlDescription(filepath.Join(root, "Cargo.toml")); d != "" {
		return d
	}
	if d := tomlDescription(filepath.Join(root, "pyproject.toml")); d != "" {
		return d
	}
	if d := jsonField(filepath.Join(root, "composer.json"), "description"); d != "" {
		return d
	}
	return readmeSummary(root)
}

// --- manifest parsers -------------------------------------------------------

func jsonField(path, field string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	if v, ok := m[field].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func goModule(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			mod := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			return filepath.Base(mod) // last path element reads as the name
		}
	}
	return ""
}

// cargoLike covers both Cargo.toml ([package]) and pyproject.toml
// ([project] and Poetry's [tool.poetry]).
type cargoLike struct {
	Package struct {
		Name        string `toml:"name"`
		Description string `toml:"description"`
	} `toml:"package"`
	Project struct {
		Name        string `toml:"name"`
		Description string `toml:"description"`
	} `toml:"project"`
	Tool struct {
		Poetry struct {
			Name        string `toml:"name"`
			Description string `toml:"description"`
		} `toml:"poetry"`
	} `toml:"tool"`
}

func parseToml(path string) (cargoLike, bool) {
	var c cargoLike
	data, err := os.ReadFile(path)
	if err != nil {
		return c, false
	}
	if _, err := toml.Decode(string(data), &c); err != nil {
		return c, false
	}
	return c, true
}

func tomlName(path string) string {
	c, ok := parseToml(path)
	if !ok {
		return ""
	}
	return first(c.Package.Name, c.Project.Name, c.Tool.Poetry.Name)
}

func tomlDescription(path string) string {
	c, ok := parseToml(path)
	if !ok {
		return ""
	}
	return first(c.Package.Description, c.Project.Description, c.Tool.Poetry.Description)
}

// readmeSummary returns the first prose line of a README, skipping headings,
// badges, images, and HTML.
func readmeSummary(root string) string {
	for _, n := range []string{"README.md", "README.markdown", "README.rst", "README.txt", "README"} {
		data, err := os.ReadFile(filepath.Join(root, n))
		if err != nil {
			continue
		}
		for _, raw := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(raw)
			if line == "" ||
				strings.HasPrefix(line, "#") ||
				strings.HasPrefix(line, "!") ||
				strings.HasPrefix(line, "<") ||
				strings.HasPrefix(line, "[!") ||
				strings.HasPrefix(line, "=") ||
				strings.HasPrefix(line, "-") {
				continue
			}
			return line
		}
	}
	return ""
}

// --- helpers ----------------------------------------------------------------

func first(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func isDir(p string) bool { fi, err := os.Stat(p); return err == nil && fi.IsDir() }
