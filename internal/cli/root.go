// Package cli implements the pgscope Cobra command tree.
package cli

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"time"

	"github.com/hghukasyan/pgscope/internal/config"
	"github.com/spf13/cobra"
)

// Version is set via -ldflags at build time.
var Version = "0.1.0-dev"

// Options holds shared CLI flags.
type Options struct {
	ConfigPath string
	DSN        string
	Format     string
	Timeout    time.Duration
	NoColor    bool
	Verbose    bool
	Config     config.Config
	Logger     *slog.Logger
	Out        io.Writer
	Err        io.Writer
}

// NewRootCommand constructs the root pgscope command.
func NewRootCommand() *cobra.Command {
	opts := &Options{
		Out: os.Stdout,
		Err: os.Stderr,
	}

	cmd := &cobra.Command{
		Use:           "pgscope",
		Short:         "Evidence-driven PostgreSQL observability and diagnostics",
		Long:          "PGScope connects to PostgreSQL, collects safe read-only observations, and produces evidence-based findings.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			cfg := config.Default()
			if opts.ConfigPath != "" {
				if err := config.LoadFile(opts.ConfigPath, &cfg); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed("format") {
				cfg.Format = opts.Format
			} else if opts.Format != "" {
				cfg.Format = opts.Format
			}
			if cmd.Flags().Changed("timeout") {
				cfg.Timeout = opts.Timeout
			}
			cfg.Color = !opts.NoColor && isTerminal(opts.Out)
			cfg.Verbose = opts.Verbose
			if err := cfg.Validate(); err != nil {
				return err
			}
			opts.Config = cfg
			opts.Logger = newLogger(opts.Verbose, opts.Err)
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&opts.ConfigPath, "config", "", "Path to YAML config file")
	cmd.PersistentFlags().StringVar(&opts.DSN, "dsn", "", "PostgreSQL connection string (or set DATABASE_URL)")
	cmd.PersistentFlags().StringVarP(&opts.Format, "format", "f", "text", "Output format: text, json, yaml")
	cmd.PersistentFlags().DurationVar(&opts.Timeout, "timeout", 30*time.Second, "Overall command timeout")
	cmd.PersistentFlags().BoolVar(&opts.NoColor, "no-color", false, "Disable ANSI colors")
	cmd.PersistentFlags().BoolVarP(&opts.Verbose, "verbose", "v", false, "Verbose / debug logging")

	cmd.AddCommand(newVersionCmd(opts))
	cmd.AddCommand(newConnectCmd(opts))
	cmd.AddCommand(newScanCmd(opts))
	cmd.AddCommand(newHealthCmd(opts))
	cmd.AddCommand(newQueriesCmd(opts))
	cmd.AddCommand(newIndexesCmd(opts))
	cmd.AddCommand(newVacuumCmd(opts))
	cmd.AddCommand(newWALCmd(opts))
	cmd.AddCommand(newReplicationCmd(opts))
	cmd.AddCommand(newVectorCmd(opts))
	cmd.AddCommand(newHistoryCmd(opts))
	cmd.AddCommand(newRegressionsCmd(opts))
	cmd.AddCommand(newIncidentsCmd(opts))
	cmd.AddCommand(newInvestigateCmd(opts))
	cmd.AddCommand(newMonitorCmd(opts))
	cmd.AddCommand(newServeCmd(opts))

	return cmd
}

func newLogger(verbose bool, w io.Writer) *slog.Logger {
	level := slog.LevelInfo
	if verbose {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}))
}

func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func (o *Options) withDSN() (config.Config, error) {
	cfg := o.Config
	if err := cfg.ApplyDSN(o.DSN); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func exitCodeForStatus(status string) int {
	switch status {
	case "critical":
		return 2
	case "warning":
		return 1
	default:
		return 0
	}
}

func printErr(w io.Writer, err error) {
	fmt.Fprintf(w, "error: %v\n", err)
}
