package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:           "hft",
		Short:         "Half-frame film scan tools",
		Long:          "hft — utilities for working with half-frame film scans.\n\n<path> may be a single image or a directory (walked recursively).",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(cutCmd(), rotateCmd(), resizeCmd())

	if err := root.Execute(); err != nil {
		// Per-file failures are already reported by the workers; only surface
		// other errors here.
		if !errors.Is(err, errHadFailures) {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		os.Exit(1)
	}
}

// addCommonFlags registers the --out and --workers flags shared by every
// command; dirSuffix names the default output directory in the help text.
func addCommonFlags(cmd *cobra.Command, out *string, workers *int, dirSuffix string) {
	cmd.Flags().StringVarP(out, "out", "o", "",
		fmt.Sprintf("output directory (default: <input>%s for dirs, alongside input for single files)", dirSuffix))
	cmd.Flags().IntVarP(workers, "workers", "j", defaultWorkers, "number of parallel workers")
}
