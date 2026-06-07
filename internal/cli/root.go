// Package cli wires the cobra command tree.
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "dev"

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "ports",
		Short:         "Track and manage the dev servers and databases you have running on local ports",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
		// Running `ports` with no subcommand defaults to `ports list`.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, args)
		},
	}
	root.AddCommand(newListCmd())
	root.AddCommand(newKillCmd())
	root.AddCommand(newTUICmd())
	root.AddCommand(newConfigCmd())
	root.AddCommand(newDoctorCmd())
	return root
}

// Execute runs the root command.
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
