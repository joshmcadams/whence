package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/spf13/cobra"

	"github.com/joshmcadams/whence/internal/config"
	"github.com/joshmcadams/whence/internal/execx"
)

func newConfigCmd() *cobra.Command {
	var doInit, doEdit bool
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Show the effective configuration, or write a default config file",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfig(os.Stdout, doInit, doEdit)
		},
	}
	cmd.Flags().BoolVar(&doInit, "init", false, "write a default config file to the config path")
	cmd.Flags().BoolVar(&doEdit, "edit", false, "open the config file in $VISUAL/$EDITOR (single binary name; falls back to vi/notepad)")
	return cmd
}

func runConfig(out io.Writer, doInit, doEdit bool) error {
	if doInit && doEdit {
		return fmt.Errorf("--init and --edit are mutually exclusive; choose one")
	}

	if doEdit {
		return runEditConfig(out)
	}

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

func runEditConfig(out io.Writer) error {
	path := config.Path()
	if _, err := os.Stat(path); err != nil {
		if _, err := config.Save(config.Default()); err != nil {
			return err
		}
		fmt.Fprintln(out, "wrote default config to", path)
	}
	editor := resolveEditor()
	if err := execx.Interactive(editor, path); err != nil {
		return fmt.Errorf("editor %q exited with error: %w", editor, err)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("after editing, %s: %w", path, err)
	}
	fmt.Fprintln(out, "# path:", path)
	return toml.NewEncoder(out).Encode(cfg)
}

func resolveEditor() string {
	for _, v := range []string{os.Getenv("VISUAL"), os.Getenv("EDITOR")} {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return fallbackEditor()
}
