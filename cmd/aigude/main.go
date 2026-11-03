// Command aigude is the AI Gude service: it turns the content you follow into
// AI-narrated audio Episodes.
package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		os.Exit(1)
	}
}

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "aigude",
		Short:         "AI Gude turns content you follow into AI-narrated audio Episodes",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newServeCommand())
	root.AddCommand(newValidateCommand())
	return root
}
