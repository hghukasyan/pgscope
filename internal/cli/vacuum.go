package cli

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/spf13/cobra"
)

func newVacuumCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vacuum",
		Short: "Autovacuum and table maintenance intelligence",
	}
	cmd.AddCommand(newVacuumInspectCmd(opts))
	cmd.AddCommand(newVacuumRisksCmd(opts))
	cmd.AddCommand(newVacuumProgressCmd(opts))
	return cmd
}

func newVacuumInspectCmd(opts *Options) *cobra.Command {
	var table string
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect table vacuum/analyze statistics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
			defer cancel()
			pool, err := database.Connect(ctx, cfg)
			if err != nil {
				return err
			}
			defer pool.Close()
			col := collector.New(pool, cfg, opts.Logger)
			stats, lims := col.CollectVacuum(ctx, table)
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"vacuum": stats, "limitations": lims})
			}
			fmt.Fprintf(opts.Out, "Vacuum inspect (tables=%d risks=%d)\n", len(stats.Tables), len(stats.Risks))
			if stats.Settings["autovacuum"] != "" {
				fmt.Fprintf(opts.Out, "autovacuum=%s freeze_max_age=%s\n", stats.Settings["autovacuum"], stats.Settings["autovacuum_freeze_max_age"])
			}
			for _, t := range stats.Tables {
				fmt.Fprintf(opts.Out, "  - %s.%s live=%d dead=%d (%.1f%%) thr=%d would_vac=%t freeze_age=%d size=%s\n",
					t.Schema, t.Name, t.LiveTuples, t.DeadTuples, t.DeadTuplePct, t.VacuumThreshold, t.WouldAutovacuum, t.RelFrozenXIDAge, t.TablePretty)
				fmt.Fprintf(opts.Out, "      threshold: %s\n", collector.FormatVacuumThresholdExplain(t))
			}
			printLims(opts, lims)
			return nil
		},
	}
	cmd.Flags().StringVar(&table, "table", "", "Limit to schema.table")
	return cmd
}

func newVacuumRisksCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "risks",
		Short: "Show tables with maintenance risk signals",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
			defer cancel()
			pool, err := database.Connect(ctx, cfg)
			if err != nil {
				return err
			}
			defer pool.Close()
			col := collector.New(pool, cfg, opts.Logger)
			stats, lims := col.CollectVacuum(ctx, "")
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"risks": stats.Risks, "long_xact_age": stats.LongXactBlockingAge, "limitations": lims})
			}
			if len(stats.Risks) == 0 {
				fmt.Fprintln(opts.Out, "No vacuum risk tables in the sampled set.")
			}
			for _, t := range stats.Risks {
				fmt.Fprintf(opts.Out, "  - %s.%s dead=%d thr=%d freeze_age=%d would_analyze=%t\n",
					t.Schema, t.Name, t.DeadTuples, t.VacuumThreshold, t.RelFrozenXIDAge, t.WouldAutoanalyze)
			}
			if stats.LongXactBlockingAge != "" {
				fmt.Fprintf(opts.Out, "Oldest transaction age: %s\n", stats.LongXactBlockingAge)
			}
			printLims(opts, lims)
			return nil
		},
	}
}

func newVacuumProgressCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "progress",
		Short: "Show active vacuum progress",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
			defer cancel()
			pool, err := database.Connect(ctx, cfg)
			if err != nil {
				return err
			}
			defer pool.Close()
			col := collector.New(pool, cfg, opts.Logger)
			stats, lims := col.CollectVacuum(ctx, "")
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"progress": stats.Progress, "limitations": lims})
			}
			if len(stats.Progress) == 0 {
				fmt.Fprintln(opts.Out, "No active vacuum progress.")
			}
			for _, p := range stats.Progress {
				fmt.Fprintf(opts.Out, "  - pid=%d %s.%s phase=%s scanned=%d/%d vacuumed=%d\n",
					p.PID, p.Schema, p.Table, p.Phase, p.HeapBlksScanned, p.HeapBlksTotal, p.HeapBlksVacuumed)
			}
			printLims(opts, lims)
			return nil
		},
	}
}
