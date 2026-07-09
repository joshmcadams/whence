package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/joshmcadams/whence/internal/config"
)

func newConfigCmd() *cobra.Command {
	var doInit bool
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show the effective configuration, or write a default config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfig(os.Stdout, doInit)
		},
	}
	cmd.Flags().BoolVar(&doInit, "init", false, "write a default config file to the config path")
	return cmd
}

func runConfig(out io.Writer, doInit bool) error {
	if doInit {
		path := config.Path()
		if _, err := os.Stat(path); err == nil {
			return fmt.Errorf("config already exists at %s (remove it first to re-init)", path)
		}
		p, err := config.Save(config.Default())
		if err != nil {
			return err
		}
		fmt.Fprintln(out, "wrote default config to", p)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Fprintln(out, "# path:", config.Path())
	if _, err := os.Stat(config.Path()); err != nil {
		fmt.Fprintln(out, "# (file not present — showing built-in defaults; run `whence config --init` to write it)")
	}
	return toml.NewEncoder(out).Encode(cfg)
}
