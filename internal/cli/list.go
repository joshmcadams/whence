package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/jmcadams/ports/internal/config"
	"github.com/jmcadams/ports/internal/inventory"
	"github.com/jmcadams/ports/internal/model"
	"github.com/jmcadams/ports/internal/output"
)

type listOpts struct {
	all      bool
	asJSON   bool
	port     int
	sortBy   string
	watch    bool
	interval time.Duration
}

func newListCmd() *cobra.Command {
	o := &listOpts{}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List your dev servers (use --all to include system/standard ports)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runListWith(o)
		},
	}
	f := cmd.Flags()
	f.BoolVarP(&o.all, "all", "a", false, "include every listening port, not just yours")
	f.BoolVar(&o.asJSON, "json", false, "output JSON")
	f.IntVarP(&o.port, "port", "p", 0, "show only this port")
	f.StringVarP(&o.sortBy, "sort", "s", "port", "sort by: port|uptime|name")
	f.BoolVarP(&o.watch, "watch", "w", false, "re-render on an interval until interrupted")
	f.DurationVarP(&o.interval, "interval", "i", 2*time.Second, "refresh interval for --watch")
	return cmd
}

// runList is the default action when `ports` is run with no subcommand.
func runList(cmd *cobra.Command, _ []string) error {
	return runListWith(&listOpts{sortBy: "port"})
}

func runListWith(o *listOpts) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if o.watch && !o.asJSON {
		return watchList(cfg, o)
	}

	servers, err := listOnce(cfg, o)
	if err != nil {
		return err
	}
	if o.asJSON {
		return output.JSON(os.Stdout, servers)
	}
	output.Table(os.Stdout, servers)
	return nil
}

func listOnce(cfg config.Config, o *listOpts) ([]model.Server, error) {
	servers, err := collect(cfg)
	if err != nil {
		return nil, err
	}
	servers = inventory.View(servers, cfg, o.all, o.port, "")
	inventory.Sort(servers, o.sortBy)
	return servers, nil
}

// watchList re-renders the table on an interval until interrupted (Ctrl-C).
func watchList(cfg config.Config, o *listOpts) error {
	for {
		servers, err := listOnce(cfg, o)
		if err != nil {
			return err
		}
		fmt.Print("\033[H\033[2J") // home + clear screen
		fmt.Printf("ports — %s (every %s, Ctrl-C to stop)\n\n",
			time.Now().Format("15:04:05"), o.interval)
		output.Table(os.Stdout, servers)
		time.Sleep(o.interval)
	}
}

func filter(in []model.Server, keep func(model.Server) bool) []model.Server {
	out := in[:0:0]
	for _, s := range in {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}
