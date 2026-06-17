package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hghukasyan/pgscope/internal/history"
	"github.com/spf13/cobra"
)

func newHistoryCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history",
		Short: "Inspect and manage local historical telemetry",
	}
	cmd.AddCommand(newHistoryStatusCmd(opts))
	cmd.AddCommand(newHistorySnapshotsCmd(opts))
	cmd.AddCommand(newHistoryMetricsCmd(opts))
	cmd.AddCommand(newHistoryCleanupCmd(opts))
	return cmd
}

func openHistory(opts *Options) (*history.SQLiteStore, error) {
	cfg := opts.Config
	cfg.History.Enabled = true
	path, err := cfg.ResolveHistoryPath()
	if err != nil {
		return nil, err
	}
	return history.OpenSQLite(context.Background(), path)
}

func newHistoryStatusCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show local history backend status",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openHistory(opts)
			if err != nil {
				return err
			}
			defer store.Close()
			st, err := store.Status(cmd.Context())
			if err != nil {
				return err
			}
			return writeJSONOrText(opts, st, func() error {
				fmt.Fprintf(opts.Out, "History backend: %s\n", st.Backend)
				fmt.Fprintf(opts.Out, "Path:            %s\n", st.Path)
				fmt.Fprintf(opts.Out, "Schema version:  %d\n", st.SchemaVersion)
				fmt.Fprintf(opts.Out, "Snapshots:       %d\n", st.SnapshotCount)
				fmt.Fprintf(opts.Out, "Metrics:         %d\n", st.MetricCount)
				fmt.Fprintf(opts.Out, "Regressions:     %d\n", st.RegressionCount)
				fmt.Fprintf(opts.Out, "Incidents:       %d\n", st.IncidentCount)
				fmt.Fprintf(opts.Out, "DB size:         %d bytes\n", st.DBSizeBytes)
				if st.OldestSnapshot != nil {
					fmt.Fprintf(opts.Out, "Oldest snapshot: %s\n", st.OldestSnapshot.Format(time.RFC3339))
				}
				if st.NewestSnapshot != nil {
					fmt.Fprintf(opts.Out, "Newest snapshot: %s\n", st.NewestSnapshot.Format(time.RFC3339))
				}
				return nil
			})
		},
	}
}

func newHistorySnapshotsCmd(opts *Options) *cobra.Command {
	var last time.Duration
	var limit int
	cmd := &cobra.Command{
		Use:   "snapshots",
		Short: "List persisted snapshots",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openHistory(opts)
			if err != nil {
				return err
			}
			defer store.Close()
			since := time.Time{}
			if last > 0 {
				since = time.Now().UTC().Add(-last)
			}
			snaps, err := store.ListSnapshots(cmd.Context(), since, time.Time{}, limit)
			if err != nil {
				return err
			}
			return writeJSONOrText(opts, snaps, func() error {
				if len(snaps) == 0 {
					fmt.Fprintln(opts.Out, "No snapshots.")
					return nil
				}
				for _, s := range snaps {
					fmt.Fprintf(opts.Out, "%s  %s  db=%s metrics=%d findings=%d (%dms)\n",
						s.CollectedAt.Format(time.RFC3339), s.ID, s.Database, s.MetricCount, s.FindingCount, s.DurationMS)
				}
				return nil
			})
		},
	}
	cmd.Flags().DurationVar(&last, "last", 24*time.Hour, "Only snapshots within this lookback")
	cmd.Flags().IntVar(&limit, "limit", 50, "Maximum snapshots to list")
	return cmd
}

func newHistoryMetricsCmd(opts *Options) *cobra.Command {
	var last time.Duration
	var name string
	var limit int
	cmd := &cobra.Command{
		Use:   "metrics",
		Short: "List persisted metric samples",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openHistory(opts)
			if err != nil {
				return err
			}
			defer store.Close()
			since := time.Time{}
			if last > 0 {
				since = time.Now().UTC().Add(-last)
			}
			ms, err := store.ListMetrics(cmd.Context(), name, since, time.Time{}, limit)
			if err != nil {
				return err
			}
			return writeJSONOrText(opts, ms, func() error {
				if len(ms) == 0 {
					fmt.Fprintln(opts.Out, "No metrics.")
					return nil
				}
				for _, m := range ms {
					fmt.Fprintf(opts.Out, "%s  %s=%.4g %s quality=%s\n",
						m.CollectedAt.Format(time.RFC3339), m.Name, m.Value, m.Unit, m.Quality)
				}
				return nil
			})
		},
	}
	cmd.Flags().DurationVar(&last, "last", time.Hour, "Lookback window")
	cmd.Flags().StringVar(&name, "name", "", "Filter by metric name")
	cmd.Flags().IntVar(&limit, "limit", 200, "Maximum samples")
	return cmd
}

func newHistoryCleanupCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "cleanup",
		Short: "Apply retention policy to local history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openHistory(opts)
			if err != nil {
				return err
			}
			defer store.Close()
			cfg := opts.Config
			stats, err := store.Cleanup(cmd.Context(), history.RetentionPolicy{
				RawMetrics: cfg.History.Retention.RawMetrics,
				Incidents:  cfg.History.Retention.Incidents,
			})
			if err != nil {
				return err
			}
			return writeJSONOrText(opts, stats, func() error {
				fmt.Fprintf(opts.Out, "Deleted metrics=%d snapshots=%d regressions=%d incidents=%d\n",
					stats.MetricsDeleted, stats.SnapshotsDeleted, stats.RegressionsDeleted, stats.IncidentsDeleted)
				return nil
			})
		},
	}
}

func writeJSONOrText(opts *Options, v any, textFn func() error) error {
	if opts.Config.Format == "json" {
		enc := json.NewEncoder(opts.Out)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
	return textFn()
}
