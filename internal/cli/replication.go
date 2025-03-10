package cli

import (
	"context"
	"fmt"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/spf13/cobra"
)

func newReplicationCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "replication",
		Short: "Replication diagnostics",
	}
	cmd.AddCommand(newReplicationInspectCmd(opts))
	cmd.AddCommand(newReplicationSlotsCmd(opts))
	cmd.AddCommand(newReplicationLagCmd(opts))
	return cmd
}

func newReplicationInspectCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "inspect",
		Short: "Inspect replication role, connections, and slots",
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
			stats, lims := col.CollectReplication(ctx)
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"replication": stats, "limitations": lims})
			}
			fmt.Fprintf(opts.Out, "Replication role=%s recovery=%v lsn=%s\n", stats.Role, stats.IsRecovery, stats.CurrentLSN)
			fmt.Fprintf(opts.Out, "Connections: %d  Slots: %d\n", len(stats.Connections), len(stats.Slots))
			for _, c := range stats.Connections {
				fmt.Fprintf(opts.Out, "  - pid=%d app=%q state=%s sync=%s\n", c.PID, c.ApplicationName, c.State, c.SyncState)
			}
			for _, s := range stats.Slots {
				fmt.Fprintf(opts.Out, "  - slot %s active=%t type=%s\n", s.SlotName, s.Active, s.SlotType)
			}
			for _, c := range stats.Conflicts {
				fmt.Fprintf(opts.Out, "  - conflicts on %s: snapshot=%d lock=%d deadlock=%d\n",
					c.Database, c.ConflSnapshot, c.ConflLock, c.ConflDeadlock)
			}
			printLims(opts, lims)
			return nil
		},
	}
}

func newReplicationSlotsCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "slots",
		Short: "List replication slots",
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
			stats, lims := col.CollectReplication(ctx)
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"slots": stats.Slots, "limitations": lims})
			}
			for _, s := range stats.Slots {
				retained := "n/a"
				if s.RetainedBytes != nil {
					retained = fmt.Sprintf("%d", *s.RetainedBytes)
				}
				fmt.Fprintf(opts.Out, "%s type=%s active=%t retained_bytes=%s wal_status=%s\n",
					s.SlotName, s.SlotType, s.Active, retained, s.WalStatus)
			}
			printLims(opts, lims)
			return nil
		},
	}
}

func newReplicationLagCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "lag",
		Short: "Show replication lag (byte and time, NULLs preserved)",
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
			stats, lims := col.CollectReplication(ctx)
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"connections": stats.Connections, "role": stats.Role, "limitations": lims})
			}
			if stats.Role != "primary" {
				fmt.Fprintf(opts.Out, "Connected role=%s — lag from pg_stat_replication is available on the primary.\n", stats.Role)
			}
			if len(stats.Connections) == 0 {
				fmt.Fprintln(opts.Out, "No replication connections visible.")
			}
			for _, c := range stats.Connections {
				byteReplay := "NULL"
				if c.ByteLagReplay != nil {
					byteReplay = fmt.Sprintf("%d", *c.ByteLagReplay)
				}
				replayLag := "NULL"
				if c.ReplayLag != nil {
					replayLag = *c.ReplayLag
				}
				fmt.Fprintf(opts.Out, "pid=%d app=%q state=%s byte_lag_replay=%s replay_lag=%s\n",
					c.PID, c.ApplicationName, c.State, byteReplay, replayLag)
			}
			printLims(opts, lims)
			return nil
		},
	}
}
