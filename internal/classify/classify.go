// Package classify scores how likely a listening server is one the user is
// actively developing, and attaches the owning project.
package classify

import (
	"strings"

	"github.com/jmcadams/ports/internal/config"
	"github.com/jmcadams/ports/internal/model"
	"github.com/jmcadams/ports/internal/project"
)

// Score weights. A server under a dev root with a repo marker clears any sane
// threshold on its own; a recognizable dev-server command adds confidence.
const (
	scoreDevRoot = 50
	scoreRepo    = 30
	scoreDevCmd  = 20
)

// devCmdHints are substrings that mark a command line as a dev server, database,
// or task runner typically launched from a repo.
var devCmdHints = []string{
	// JS/TS
	"vite", "next", "webpack", "nodemon", "ts-node", "tsx", "ng serve",
	"astro", "remix", "nest start", "nuxt", "npm run", "yarn dev", "pnpm dev",
	"pnpm run", "yarn run", "bun run",
	// Python
	"runserver", "flask", "uvicorn", "gunicorn", "hypercorn", "fastapi",
	"manage.py", "celery",
	// Ruby / PHP / Elixir
	"rails", "puma", "rackup", "bundle exec", "artisan serve", "php -S",
	"phx.server", "mix ",
	// Go / Rust / JVM
	"air", "dlv", "go run", "cargo run", "cargo watch", "gradlew bootrun",
	"spring-boot", "quarkus",
	// Databases run locally from a project
	"postgres", "postgresql", "mysqld", "mariadb", "mongod", "redis-server",
	"node ",
}

// Process scores and annotates native-process servers in place. It resolves the
// project from each server's cwd. Docker servers are scored in the docker
// package and should not be passed here.
func Process(servers []model.Server, cfg config.Config) {
	for i := range servers {
		s := &servers[i]
		if s.Source != model.SourceProcess {
			continue
		}
		if s.Cwd != "" {
			s.Project = project.Detect(s.Cwd)
		}
		s.Confidence = scoreProcess(*s, cfg)
	}
}

func scoreProcess(s model.Server, cfg config.Config) int {
	score := 0
	if cfg.IsUnderDevRoot(s.Cwd) {
		score += scoreDevRoot
	}
	if s.Project != nil {
		score += scoreRepo
	}
	if looksLikeDevCmd(s.Cmdline) {
		score += scoreDevCmd
	}
	if score > 100 {
		score = 100
	}
	return score
}

func looksLikeDevCmd(cmdline string) bool {
	c := strings.ToLower(cmdline)
	for _, h := range devCmdHints {
		if strings.Contains(c, h) {
			return true
		}
	}
	return false
}

// Mine reports whether a server clears the configured confidence threshold.
func Mine(s model.Server, cfg config.Config) bool {
	return s.Confidence >= cfg.ConfidenceThreshold
}
