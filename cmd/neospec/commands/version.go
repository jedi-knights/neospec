package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewVersionCmd returns the `neospec version` command.
func NewVersionCmd(version string) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print neospec version",
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println("neospec", version)
		},
	}
}
