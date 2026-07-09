package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/inventory"
	"github.com/joshmcadams/whence/internal/model"
	"github.com/joshmcadams/whence/internal/output"
)

type listOpts struct {
	all      bool
	asJSON   bool
	port     int
	sortBy   string
	watch    bool
	interval time.Duration
	noIgnore bool
}

func newListCmd() *cobra.Command {
	o := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list [query]",
		Short: "List dev servers; optionally filter by a query (name, description, or port)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := ""
			if len(args) > 0 {
				query = args[0]
			}
			return runListWith(o, query)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&o.all, "all", "a", false, "include every listening port, not just yours")
	f.BoolVar(&o.asJSON, "json", false, "output JSON")
	f.IntVarP(&o.port, "port", "p", 0, "show only this port")
	f.StringVarP(&o.sortBy, "sort", "s", "port", "sort by: port|uptime|name")
	f.BoolVarP(&o.watch, "watch", "w", false, "re-render on an interval until interrupted")
	f.DurationVarP(&o.interval, "interval", "i", 2*time.Second, "refresh interval for --watch")
	f.BoolVar(&o.noIgnore, "no-ignore", false, "bypass the configured ignore_ports/ignore_names lists")
	return cmd
}

// runList is the default action when `whence` is run with no subcommand.
func runList(cmd *cobra.Command, _ []string) error {
	return runListWith(&listOpts{sortBy: "port"}, "")
}

func runListWith(o *listOpts, query string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if o.watch {
		if o.asJSON {
			return errors.New("--watch cannot be combined with --json")
		}
		if o.interval < 500*time.Millisecond {
			return fmt.Errorf("--interval must be at least 500ms (got %s)", o.interval)
		}
		return watchList(cfg, o, query)
	}

	servers, hidden, err := listOnce(cfg, o, query)
	if err != nil {
		return err
	}
	if o.asJSON {
		return output.JSON(os.Stdout, servers)
	}
	output.Table(os.Stdout, servers, hidden)
	return nil
}

func listOnce(cfg config.Config, o *listOpts, query string) ([]model.Server, int, error) {
	raw, err := collect(cfg)
	if err != nil {
		return nil, 0, err
	}
	if o.noIgnore {
		// cfg is a value copy; clearing the lists disables ignore filtering in
		// View for this call only, without touching the loaded config.
		cfg.IgnorePorts = nil
		cfg.IgnoreNames = nil
	}
	servers := inventory.View(raw, cfg, o.all, o.port, query)
	inventory.Sort(servers, o.sortBy)

	hidden := 0
	if !o.all {
		allView := inventory.View(raw, cfg, true, o.port, query)
		hidden = len(allView) - len(servers)
	}
	return servers, hidden, nil
}

// watchList re-renders the table on an interval until interrupted (Ctrl-C).
// Each frame is buffered then written line-by-line with an erase-to-end-of-line
// escape (\033[K) so the terminal never flashes blank and trailing characters
// from a wider previous frame don't bleed through.
func watchList(cfg config.Config, o *listOpts, query string) error {
	for {
		servers, hidden, err := listOnce(cfg, o, query)
		if err != nil {
			return err
		}

		var buf bytes.Buffer
		fmt.Fprintf(&buf, "whence — %s (every %s, Ctrl-C to stop)\n\n",
			time.Now().Format("15:04:05"), o.interval)
		output.Table(&buf, servers, hidden)

		fmt.Print("\033[H") // cursor home; no blank-screen clear
		for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
			fmt.Print(line, "\033[K\n") // overwrite + erase any old tail on this line
		}
		fmt.Print("\033[J") // erase any leftover lines below the new content

		time.Sleep(o.interval)
	}
}
