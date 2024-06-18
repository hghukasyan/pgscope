package cli

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/internal/diagnosis"
	"github.com/hghukasyan/pgscope/internal/report"
	"github.com/spf13/cobra"
)

func newScanCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "scan",
		Short: "Collect observations and emit evidence-based diagnostics",
		Long:  "Connects read-only to PostgreSQL, collects connection/transaction/lock/storage/index observations, evaluates diagnostic rules, and prints a report.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
			defer cancel()

			opts.Logger.Debug("connecting", "dsn", database.RedactDSN(cfg.DSN))
			pool, err := database.Connect(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect failed (dsn=%s): %w", database.RedactDSN(cfg.DSN), err)
			}
			defer pool.Close()

			col := collector.New(pool, cfg, opts.Logger)
			snap, err := col.Collect(ctx)
			if err != nil {
				return fmt.Errorf("collect: %w", err)
			}

			engine := diagnosis.New()
			result, err := engine.Analyze(ctx, snap)
			if err != nil {
				return fmt.Errorf("diagnose: %w", err)
			}

			if err := report.Write(opts.Out, result, cfg.Format, cfg.Color); err != nil {
				return err
			}

			code := exitCodeForStatus(result.OverallStatus)
			if code != 0 {
				cmd.SilenceUsage = true
				return &exitError{code: code}
			}
			return nil
		},
	}
}

// exitError carries a process exit code without printing an extra error line.
type exitError struct {
	code int
}

func (e *exitError) Error() string {
	return fmt.Sprintf("exit status %d", e.code)
}

func (e *exitError) ExitCode() int {
	return e.code
}
