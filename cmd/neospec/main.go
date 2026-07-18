// neospec is a self-contained test runner and coverage tool for Neovim plugins
// and distributions. It downloads and caches Neovim binaries automatically —
// no system Neovim installation is required.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/jedi-knights/neospec/cmd/neospec/commands"
)

var version = "dev" // overridden at build time with -ldflags

func main() {
	root := &cobra.Command{
		Use:   "neospec",
		Short: "Test runner and coverage tool for Neovim plugins",
		Long: `neospec runs Lua test suites for Neovim plugins and distributions.
It downloads and caches Neovim binaries automatically — no system install required.`,
		SilenceUsage: true,
	}

	root.AddCommand(
		commands.NewRunCmd(),
		commands.NewExecCmd(),
		commands.NewVersionCmd(version),
		commands.NewCacheCmd(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
