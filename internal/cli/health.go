package cli

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/internal/diagnosis"
	"github.com/hghukasyan/pgscope/internal/report"
	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/spf13/cobra"
)

func newHealthCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "health",
		Short: "Quick health check focusing on connections, locks, and transactions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
			defer cancel()

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

			if cfg.Format == "text" {
				fmt.Fprintf(opts.Out, "Health: %s\n", result.OverallStatus)
				fmt.Fprintf(opts.Out, "%s\n", result.Summary)
				if snap.Connections.Collected {
					fmt.Fprintf(opts.Out, "Connections: %d/%d (%.1f%%) active=%d idle=%d idle_in_xact=%d\n",
						snap.Connections.Total, snap.Connections.MaxConnections, snap.Connections.UtilizationPct,
						snap.Connections.Active, snap.Connections.Idle, snap.Connections.IdleInTransaction)
				}
				if snap.Locks.Collected {
					fmt.Fprintf(opts.Out, "Locks waiting: %d  chains: %d\n", snap.Locks.WaitingCount, len(snap.Locks.BlockingChains))
				}
				for _, f := range result.Findings {
					if f.Severity == models.SeverityInfo {
						continue
					}
					fmt.Fprintf(opts.Out, "- [%s] %s: %s\n", f.Severity, f.RuleID, f.Title)
				}
			} else if err := report.Write(opts.Out, result, cfg.Format, cfg.Color); err != nil {
				return err
			}

			code := exitCodeForStatus(result.OverallStatus)
			if code != 0 {
				return &exitError{code: code}
			}
			return nil
		},
	}
}
