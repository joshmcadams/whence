package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/kill"
	"github.com/joshmcadams/whence/internal/model"
)

type killOpts struct {
	force   bool
	single  bool
	timeout time.Duration
}

func newKillCmd() *cobra.Command {
	o := &killOpts{}
	cmd := &cobra.Command{
		Use:   "kill <port|name>",
		Short: "Kill the server on a port, or all servers in a project",
		Long: "Kill by port number (e.g. `whence kill 3000`) or by project/server name\n" +
			"(e.g. `whence kill nexxus`). A name prefers an exact (case-insensitive)\n" +
			"match and only falls back to substring when there is none. Native\n" +
			"processes are killed as a tree (SIGTERM then SIGKILL); compose services\n" +
			"are stopped via docker.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKill(args[0], o)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&o.force, "force", "f", false, "skip the confirmation prompt")
	f.BoolVar(&o.single, "single", false, "kill only the listening process, not its launcher tree; for a name match with multiple targets each listener is killed individually")
	f.DurationVarP(&o.timeout, "timeout", "t", 0, "grace period before force-kill (default from config)")
	return cmd
}

// killDeps supplies runKillWith with everything it needs beyond the target and
// flags: the collected inventory, the kill function, and the I/O streams for
// the confirmation prompt and progress output. Split out so tests can inject
// fixtures and buffers instead of the real config/inventory/process table.
type killDeps struct {
	cfg     config.Config
	servers []model.Server
	kill    func(model.Server, kill.Opts) kill.Result
	in      io.Reader
	out     io.Writer
	errOut  io.Writer
}

func runKill(target string, o *killOpts) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	servers, err := collect(cfg)
	if err != nil {
		return err
	}
	return runKillWith(target, o, killDeps{
		cfg:     cfg,
		servers: servers,
		kill:    kill.Server,
		in:      os.Stdin,
		out:     os.Stdout,
		errOut:  os.Stderr,
	})
}

func runKillWith(target string, o *killOpts, d killDeps) error {
	matches, fuzzy := matchTargets(d.servers, target)
	if len(matches) == 0 {
		return fmt.Errorf("no server found matching %q", target)
	}
	units := dedupeUnits(matches)

	// Warn when --single is paired with a multi-unit name match: each listener
	// is killed by PID alone, leaving the rest of its process tree running.
	if o.single && len(units) > 1 {
		if port, err := strconv.Atoi(target); err != nil || port <= 0 {
			fmt.Fprintf(d.errOut, "note: --single with %d matched targets kills only each listener pid, not its tree\n", len(units))
		}
	}

	timeout := o.timeout
	if timeout <= 0 {
		timeout = time.Duration(d.cfg.KillTimeoutSeconds) * time.Second
	}
	opts := kill.Opts{Timeout: timeout, Single: o.single}

	// Confirm unless forced, previewing the full tree each kill will signal.
	if !o.force {
		if !confirmKill(units, target, fuzzy, opts, d.out, d.in) {
			fmt.Fprintln(d.out, "Aborted.")
			return nil
		}
	}

	var failed int
	for _, s := range units {
		res := d.kill(s, opts)
		if res.Err != nil {
			failed++
			fmt.Fprintf(d.out, "✗ %s — %v\n", describe(s), res.Err)
		} else {
			fmt.Fprintf(d.out, "✓ killed %s (%s)\n", describe(s), res.Method)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d kill(s) failed", failed, len(units))
	}
	return nil
}

// matchTargets selects servers by port (if target is a positive integer) or by
// name. Name matching prefers exact (case-insensitive) matches on the project,
// display, or process/container name; only when there are none does it fall back
// to substring, returning fuzzy=true so the confirmation can flag the looser
// match. Exact-first keeps `kill api` from also taking `api-gateway` down.
func matchTargets(servers []model.Server, target string) (matches []model.Server, fuzzy bool) {
	if port, err := strconv.Atoi(target); err == nil && port > 0 {
		return filter(servers, func(s model.Server) bool { return s.Port == port }), false
	}
	want := strings.ToLower(target)
	if exact := filter(servers, func(s model.Server) bool { return nameMatches(s, want, true) }); len(exact) > 0 {
		return exact, false
	}
	return filter(servers, func(s model.Server) bool { return nameMatches(s, want, false) }), true
}

// nameMatches reports whether any of a server's names equals (exact) or contains
// (substring) want; comparison is case-insensitive.
func nameMatches(s model.Server, want string, exact bool) bool {
	names := []string{s.DisplayName(), s.Name}
	if s.Project != nil {
		names = append(names, s.Project.Name)
	}
	for _, n := range names {
		n = strings.ToLower(n)
		if n == "" {
			continue
		}
		if exact {
			if n == want {
				return true
			}
		} else if strings.Contains(n, want) {
			return true
		}
	}
	return false
}

// dedupeUnits collapses servers that map to the same kill action: native
// processes by PID, containers by name. (One process can hold several ports.)
func dedupeUnits(servers []model.Server) []model.Server {
	seenPID := map[int]bool{}
	seenContainer := map[string]bool{}
	var out []model.Server
	for _, s := range servers {
		if s.Source == model.SourceDocker {
			if seenContainer[s.Name] {
				continue
			}
			seenContainer[s.Name] = true
		} else {
			if s.PID > 0 && seenPID[s.PID] {
				continue
			}
			if s.PID > 0 {
				seenPID[s.PID] = true
			}
		}
		out = append(out, s)
	}
	return out
}

// confirmKill previews the actual process tree each kill will signal — not just
// the listening pid — then asks for confirmation. Because a kill climbs to a
// launcher and takes the whole subtree, one listening server can mean several
// processes; the user sees them all before agreeing.
func confirmKill(units []model.Server, target string, fuzzy bool, opts kill.Opts, out io.Writer, in io.Reader) bool {
	plans := kill.PreviewBatch(units, opts)
	totalProcs := 0
	for _, p := range plans {
		totalProcs += len(p.Tree)
	}

	if fuzzy {
		fmt.Fprintf(out, "No exact match for %q; %d server(s) contain it", target, len(units))
	} else {
		fmt.Fprintf(out, "About to kill %d target(s) matching %q", len(units), target)
	}
	if totalProcs > 0 {
		fmt.Fprintf(out, " — %d process(es) total", totalProcs)
	}
	fmt.Fprintln(out, ":")
	for _, p := range plans {
		printPlan(p, out)
	}
	return confirm("Proceed? [y/N] ", out, in)
}

func printPlan(p kill.Plan, out io.Writer) {
	fmt.Fprintf(out, "  %s\n", describe(p.Server))
	for _, line := range p.Lines() {
		fmt.Fprintf(out, "      %s\n", line)
	}
}

func describe(s model.Server) string {
	name := s.DisplayName()
	if name == "" {
		name = "(unknown)"
	}
	switch s.Source {
	case model.SourceDocker:
		return fmt.Sprintf(":%d %s [container %s]", s.Port, name, s.Name)
	default:
		return fmt.Sprintf(":%d %s [pid %d]", s.Port, name, s.PID)
	}
}

func confirm(prompt string, out io.Writer, in io.Reader) bool {
	fmt.Fprint(out, prompt)
	r := bufio.NewReader(in)
	line, err := r.ReadString('\n')
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
