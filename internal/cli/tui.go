package cli

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/tui"
)

func newTUICmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Interactive table: navigate with arrows, x to kill, enter for details",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			p := tea.NewProgram(tui.New(cfg, all), tea.WithAltScreen())
			_, err = p.Run()
			return err
		},
	}
	cmd.Flags().BoolVarP(&all, "all", "a", false, "start showing all ports, not just yours")
	return cmd
}
