package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/internal/history"
	"github.com/hghukasyan/pgscope/internal/monitor"
	"github.com/spf13/cobra"
)

func newMonitorCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "monitor",
		Short: "Continuous PostgreSQL monitoring with historical telemetry",
	}
	cmd.AddCommand(newMonitorStartCmd(opts))
	cmd.AddCommand(newMonitorOnceCmd(opts))
	return cmd
}

func newServeCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Run continuous monitoring service (alias of monitor start)",
		RunE:  newMonitorStartCmd(opts).RunE,
	}
}

func newMonitorStartCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start continuous monitoring until interrupted",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}
			cfg.History.Enabled = true
			cfg.Monitoring.Enabled = true
			if err := cfg.Validate(); err != nil {
				return err
			}

			path, err := cfg.ResolveHistoryPath()
			if err != nil {
				return err
			}
			store, err := history.OpenSQLite(cmd.Context(), path)
			if err != nil {
				return err
			}
			defer store.Close()

			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			pool, err := database.Connect(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect failed (dsn=%s): %w", database.RedactDSN(cfg.DSN), err)
			}
			defer pool.Close()

			col := collector.New(pool, cfg, opts.Logger)
			svc := monitor.New(cfg, col, store, opts.Logger)
			fmt.Fprintf(opts.Out, "PGScope monitor running (history=%s interval=%s). Ctrl-C to stop.\n",
				path, cfg.Monitoring.DefaultInterval)
			err = svc.Run(ctx)
			if err == context.Canceled {
				st := svc.Stats()
				fmt.Fprintf(opts.Out, "Stopped. cycles=%d snapshots=%d regressions=%d incidents=%d errors=%d\n",
					st.Cycles, st.SnapshotsSaved, st.RegressionsFound, st.IncidentsTouched, st.CollectionErrors)
				return nil
			}
			return err
		},
	}
}

func newMonitorOnceCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "once",
		Short: "Run a single monitoring cycle (collect, persist, detect)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}
			cfg.History.Enabled = true
			path, err := cfg.ResolveHistoryPath()
			if err != nil {
				return err
			}
			store, err := history.OpenSQLite(cmd.Context(), path)
			if err != nil {
				return err
			}
			defer store.Close()

			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
			defer cancel()

			pool, err := database.Connect(ctx, cfg)
			if err != nil {
				return err
			}
			defer pool.Close()

			col := collector.New(pool, cfg, opts.Logger)
			svc := monitor.New(cfg, col, store, opts.Logger)
			if err := svc.RunOnce(ctx); err != nil {
				return err
			}
			st := svc.Stats()
			fmt.Fprintf(opts.Out, "Cycle complete in %dms (snapshot_saved=%v collection_errors=%d)\n",
				st.LastDurationMS, st.SnapshotsSaved > 0, st.CollectionErrors)
			_ = time.Second
			return nil
		},
	}
}
