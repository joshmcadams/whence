package cli

import (
	"fmt"
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
			if doInit {
				if _, err := os.Stat(config.Path()); err == nil {
					return fmt.Errorf("config already exists at %s (remove it first to re-init)", config.Path())
				}
				p, err := config.Save(config.Default())
				if err != nil {
					return err
				}
				fmt.Println("wrote default config to", p)
				return nil
			}

			cfg, err := config.Load()
			if err != nil {
				return err
			}
			fmt.Println("# path:", config.Path())
			if _, err := os.Stat(config.Path()); err != nil {
				fmt.Println("# (file not present — showing built-in defaults; run `whence config --init` to write it)")
			}
			return toml.NewEncoder(os.Stdout).Encode(cfg)
		},
	}
	cmd.Flags().BoolVar(&doInit, "init", false, "write a default config file to the config path")
	return cmd
}
