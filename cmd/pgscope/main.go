package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/hghukasyan/pgscope/internal/cli"
)

func main() {
	root := cli.NewRootCommand()
	if err := root.Execute(); err != nil {
		var ex interface{ ExitCode() int }
		if errors.As(err, &ex) {
			os.Exit(ex.ExitCode())
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
