package cli

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/spf13/cobra"
)

func newWALCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wal",
		Short: "WAL and checkpoint intelligence",
	}
	cmd.AddCommand(newWALInspectCmd(opts))
	cmd.AddCommand(newWALSlotsCmd(opts))
	cmd.AddCommand(newWALCheckpointsCmd(opts))
	return cmd
}

func newWALInspectCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect",
		Short: "Inspect WAL generation and related settings",
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
			stats, lims := col.CollectWAL(ctx)
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"wal": stats, "limitations": lims})
			}
			fmt.Fprintln(opts.Out, "WAL inspect")
			fmt.Fprintf(opts.Out, "  current_lsn: %s\n", stats.CurrentLSN)
			if stats.WALBytes != nil {
				fmt.Fprintf(opts.Out, "  wal_bytes: %d\n", *stats.WALBytes)
			}
			if stats.WALRecords != nil {
				fmt.Fprintf(opts.Out, "  wal_records: %d\n", *stats.WALRecords)
			}
			if stats.WALBuffersFull != nil {
				fmt.Fprintf(opts.Out, "  wal_buffers_full: %d\n", *stats.WALBuffersFull)
			}
			for k, v := range stats.Settings {
				fmt.Fprintf(opts.Out, "  setting %s=%s\n", k, v)
			}
			fmt.Fprintln(opts.Out, "Note: rate conclusions require two valid snapshots (`DeriveWALRates`).")
			printLims(opts, lims)
			return nil
		},
	}
}

func newWALSlotsCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "slots",
		Short: "Show replication slots and WAL retention",
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
			repl, lims := col.CollectReplication(ctx)
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"slots": repl.Slots, "limitations": lims})
			}
			fmt.Fprintf(opts.Out, "Replication slots (%d) role=%s\n", len(repl.Slots), repl.Role)
			for _, s := range repl.Slots {
				retained := "n/a"
				if s.RetainedBytes != nil {
					retained = fmt.Sprintf("%d", *s.RetainedBytes)
				}
				fmt.Fprintf(opts.Out, "  - %s type=%s active=%t wal_status=%s retained_bytes=%s restart_lsn=%s\n",
					s.SlotName, s.SlotType, s.Active, s.WalStatus, retained, s.RestartLSN)
			}
			printLims(opts, lims)
			return nil
		},
	}
}

func newWALCheckpointsCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "checkpoints",
		Short: "Show checkpoint counters",
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
			stats, lims := col.CollectWAL(ctx)
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"wal": stats, "limitations": lims})
			}
			fmt.Fprintln(opts.Out, "Checkpoints")
			if stats.CheckpointsTimed != nil {
				fmt.Fprintf(opts.Out, "  timed: %d\n", *stats.CheckpointsTimed)
			}
			if stats.CheckpointsReq != nil {
				fmt.Fprintf(opts.Out, "  requested: %d\n", *stats.CheckpointsReq)
			}
			if stats.CheckpointsReqRatio != nil {
				fmt.Fprintf(opts.Out, "  requested_ratio: %.1f%%\n", *stats.CheckpointsReqRatio*100)
			}
			if stats.CheckpointWriteTimeMS != nil {
				fmt.Fprintf(opts.Out, "  write_time_ms: %.1f\n", *stats.CheckpointWriteTimeMS)
			}
			if stats.CheckpointSyncTimeMS != nil {
				fmt.Fprintf(opts.Out, "  sync_time_ms: %.1f\n", *stats.CheckpointSyncTimeMS)
			}
			printLims(opts, lims)
			return nil
		},
	}
}
