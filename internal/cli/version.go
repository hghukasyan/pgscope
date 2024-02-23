package cli

import (
	"fmt"
	"runtime"

	"github.com/spf13/cobra"
)

func newVersionCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print PGScope version",
		RunE: func(cmd *cobra.Command, _ []string) error {
			fmt.Fprintf(opts.Out, "pgscope %s\n", Version)
			fmt.Fprintf(opts.Out, "go: %s\n", runtime.Version())
			fmt.Fprintf(opts.Out, "os/arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
			return nil
		},
	}
}
