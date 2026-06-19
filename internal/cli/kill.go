package cli

import (
	"bufio"
	"fmt"
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
			"(e.g. `whence kill nexxus`). Native processes are killed as a tree\n" +
			"(SIGTERM then SIGKILL); compose services are stopped via docker.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runKill(args[0], o)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&o.force, "force", "f", false, "skip the confirmation prompt")
	f.BoolVar(&o.single, "single", false, "kill only the listening process, not its tree")
	f.DurationVarP(&o.timeout, "timeout", "t", 0, "grace period before force-kill (default from config)")
	return cmd
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

	matches := matchTargets(servers, target)
	if len(matches) == 0 {
		return fmt.Errorf("no server found matching %q", target)
	}
	units := dedupeUnits(matches)

	timeout := o.timeout
	if timeout <= 0 {
		timeout = time.Duration(cfg.KillTimeoutSeconds) * time.Second
	}
	opts := kill.Opts{Timeout: timeout, Single: o.single}

	// Confirm unless forced, previewing the full tree each kill will signal.
	if !o.force {
		if !confirmKill(units, target, opts) {
			fmt.Println("Aborted.")
			return nil
		}
	}

	var failed int
	for _, s := range units {
		res := kill.Server(s, opts)
		if res.Err != nil {
			failed++
			fmt.Printf("✗ %s — %v\n", describe(s), res.Err)
		} else {
			fmt.Printf("✓ killed %s (%s)\n", describe(s), res.Method)
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d kill(s) failed", failed, len(units))
	}
	return nil
}

// matchTargets selects servers by port (if target is a positive integer) or by
// case-insensitive name (project name, display name, or container/process name).
func matchTargets(servers []model.Server, target string) []model.Server {
	if port, err := strconv.Atoi(target); err == nil && port > 0 {
		return filter(servers, func(s model.Server) bool { return s.Port == port })
	}
	want := strings.ToLower(target)
	return filter(servers, func(s model.Server) bool {
		if strings.Contains(strings.ToLower(s.DisplayName()), want) {
			return true
		}
		if strings.Contains(strings.ToLower(s.Name), want) {
			return true
		}
		return s.Project != nil && strings.Contains(strings.ToLower(s.Project.Name), want)
	})
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
func confirmKill(units []model.Server, target string, opts kill.Opts) bool {
	plans := make([]kill.Plan, len(units))
	totalProcs := 0
	for i, s := range units {
		plans[i] = kill.Preview(s, opts)
		totalProcs += len(plans[i].Tree)
	}

	fmt.Printf("About to kill %d target(s) matching %q", len(units), target)
	if totalProcs > 0 {
		fmt.Printf(" — %d process(es) total", totalProcs)
	}
	fmt.Println(":")
	for _, p := range plans {
		printPlan(p)
	}
	return confirm("Proceed? [y/N] ")
}

func printPlan(p kill.Plan) {
	fmt.Printf("  %s\n", describe(p.Server))
	for _, line := range p.Lines() {
		fmt.Printf("      %s\n", line)
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

func confirm(prompt string) bool {
	fmt.Print(prompt)
	r := bufio.NewReader(os.Stdin)
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
