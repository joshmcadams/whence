package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
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

	row("platform", fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))
	row("go version", runtime.Version())
	row("config path", config.Path())
	if _, err := os.Stat(config.Path()); err == nil {
		row("config file", "found")
	} else {
		row("config file", "not present (using defaults)")
	}

	// macOS leans on lsof for cwd (and possibly socket enumeration).
	if runtime.GOOS == "darwin" {
		if path, err := exec.LookPath("lsof"); err == nil {
			row("lsof", "found at "+path)
		} else {
			row("lsof", "MISSING — cwd resolution will fail on macOS")
		}
	}

	// Docker path: availability and how many published ports we attributed.
	if docker.Available() {
		dockers, err := docker.Servers()
		if err != nil {
			row("docker", "found, but query failed: "+err.Error())
		} else {
			compose := 0
			for _, d := range dockers {
				if d.Project != nil {
					compose++
				}
			}
			row("docker", fmt.Sprintf("found — %d published port(s), %d compose-attributed", len(dockers), compose))
		}
	} else {
		row("docker", "not found (compose services won't be detected)")
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
