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

// Cache memoises project detection keyed by resolved repo root so that multiple
// servers sharing a root (e.g. a monorepo's web + api processes) only pay the
// disk-read cost once per Collect invocation.
type Cache struct {
	byRoot map[string]*model.Project
}

// NewCache returns an empty Cache ready for a single Collect invocation.
func NewCache() *Cache { return &Cache{byRoot: map[string]*model.Project{}} }

// Detect is the memoised form of the package-level Detect function.
func (c *Cache) Detect(startDir string) *model.Project {
	if startDir == "" {
		return nil
	}
	root, marker := findRoot(startDir)
	if root == "" {
		return nil
	}
	if p, ok := c.byRoot[root]; ok {
		return p
	}
	p := &model.Project{
		Name:        name(root),
		Root:        root,
		Description: Description(root),
		Marker:      marker,
	}
	c.byRoot[root] = p
	return p
}

// manifests are non-.git markers that also indicate a project root, in priority
// order (used when no .git is found while walking up).
var manifests = []string{
	"go.mod", "package.json", "pyproject.toml", "Cargo.toml",
	"composer.json", "Gemfile", "build.gradle", "pom.xml", "Makefile",
}

func findRoot(start string) (root, marker string) {
	dir := filepath.Clean(start)
	var firstManifest, firstManifestMarker string
	for {
		if exists(filepath.Join(dir, ".git")) {
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

// maxManifestBytes bounds any single attribution read. Real manifests and
// READMEs are tiny; the cap defends against reading a huge or non-regular
// file (e.g. a FIFO, which would block forever) chosen by an untrusted
// process cwd or docker compose label.
const maxManifestBytes = 1 << 20 // 1 MiB

// readSmallFile reads path only if Lstat reports a regular file no larger
// than maxManifestBytes. Using Lstat (not Stat) means a symlink is rejected
// even when it points at a legitimate manifest/README — a small, accepted
// behavior change (a symlinked README now yields no description) in
// exchange for never following a link into something unbounded.
func readSmallFile(path string) ([]byte, bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxManifestBytes {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > maxManifestBytes {
		return nil, false
	}
	return data, true
}

func jsonField(path, field string) string {
	data, ok := readSmallFile(path)
	if !ok {
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
	data, ok := readSmallFile(path)
	if !ok {
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
	data, ok := readSmallFile(path)
	if !ok {
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
		data, ok := readSmallFile(filepath.Join(root, n))
		if !ok {
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
