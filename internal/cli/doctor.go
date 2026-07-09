package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/docker"
	"github.com/joshmcadams/whence/internal/scan"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Report platform capabilities, privileges, and missing dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDoctor()
		},
	}
}

func runDoctor() error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	row := func(k, v string) { fmt.Fprintf(tw, "%s\t%s\n", k, v) }

	row("whence version", version)
	row("platform", fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))
	row("go version", runtime.Version())
	row("config path", config.Path())
	if _, err := os.Stat(config.Path()); err == nil {
		row("config file", "found")
	} else {
		row("config file", "not present (using defaults)")
	}

	// Surface active ignore lists: they suppress entries even under --all, so
	// this is where a user learns why something they expected isn't listed.
	cfg, _ := config.Load()
	if len(cfg.IgnorePorts) > 0 {
		row("ignored ports", fmt.Sprintf("%v (bypass with list --no-ignore)", cfg.IgnorePorts))
	}
	if len(cfg.IgnoreNames) > 0 {
		row("ignored names", strings.Join(cfg.IgnoreNames, ", ")+" (bypass with list --no-ignore)")
	}

	// macOS requires lsof for both socket enumeration and cwd resolution.
	// gopsutil shells out to lsof inside gnet.Connections for the socket scan
	// itself, not just for cwd — so without lsof, whence cannot list servers
	// at all on macOS.
	if runtime.GOOS == "darwin" {
		if path, err := exec.LookPath("lsof"); err == nil {
			row("lsof", "found at "+path)
		} else {
			row("lsof", "MISSING — socket enumeration and cwd resolution both require lsof on macOS")
		}
	}

	// Docker path: availability and how many published ports we attributed.
	if docker.Available() {
		runtime := docker.Runtime()
		dockers, err := docker.Servers()
		if err != nil {
			row("container runtime", runtime+": found, but query failed: "+err.Error())
		} else {
			compose := 0
			for _, d := range dockers {
				if d.Project != nil {
					compose++
				}
			}
			row("container runtime", fmt.Sprintf("%s — %d published port(s), %d compose-attributed", runtime, len(dockers), compose))
		}
	} else {
		row("container runtime", "not found (compose services won't be detected)")
	}

	// Live probe: can we enumerate sockets, and how many did we attribute?
	servers, err := scan.Processes()
	if err != nil {
		row("socket scan", "FAILED: "+err.Error())
		tw.Flush()
		return nil
	}
	attributed, withCwd := 0, 0
	for _, s := range servers {
		if s.Attributed() {
			attributed++
		}
		if s.Cwd != "" {
			withCwd++
		}
	}
	row("listening ports", fmt.Sprintf("%d", len(servers)))
	row("with owning pid", fmt.Sprintf("%d", attributed))
	row("with resolved cwd", fmt.Sprintf("%d", withCwd))
	if attributed < len(servers) {
		row("note", "some ports lack a pid — owned by another user; rerun elevated to see them")
	}

	tw.Flush()
	return nil
}
