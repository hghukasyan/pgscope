package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/internal/explain"
	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func newQueriesCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queries",
		Short: "Query intelligence (pg_stat_statements + EXPLAIN)",
	}
	cmd.AddCommand(newQueriesTopCmd(opts))
	cmd.AddCommand(newQueriesExplainCmd(opts))
	return cmd
}

func newQueriesTopCmd(opts *Options) *cobra.Command {
	var sort string
	var limit int
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Show top statements from pg_stat_statements",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}
			if cmd.Flags().Changed("sort") {
				cfg.QuerySort = sort
			}
			if cmd.Flags().Changed("limit") {
				cfg.TopQueries = limit
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), cfg.Timeout)
			defer cancel()

			pool, err := database.Connect(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect failed (dsn=%s): %w", database.RedactDSN(cfg.DSN), err)
			}
			defer pool.Close()

			col := collector.New(pool, cfg, opts.Logger)
			stats, lims := col.CollectQueries(ctx, models.QuerySortKey(cfg.QuerySort), cfg.TopQueries)

			if cfg.Format == "json" || cfg.Format == "yaml" {
				payload := map[string]any{"queries": stats, "limitations": lims}
				return encodeOut(opts.Out, cfg.Format, payload)
			}

			if !stats.Available || !stats.Collected {
				fmt.Fprintln(opts.Out, "Query intelligence unavailable.")
				for _, l := range lims {
					fmt.Fprintf(opts.Out, "  [%s] %s — %s\n", l.Area, l.Reason, l.Impact)
					if l.Hint != "" {
						fmt.Fprintf(opts.Out, "    Hint: %s\n", l.Hint)
					}
				}
				return nil
			}

			fmt.Fprintf(opts.Out, "Top queries (sort=%s limit=%d)\n", stats.SortBy, stats.Limit)
			if stats.StatsReset != nil {
				fmt.Fprintf(opts.Out, "stats_reset: %s\n", stats.StatsReset.UTC().Format("2006-01-02T15:04:05Z"))
			}
			fmt.Fprintf(opts.Out, "%-18s %10s %12s %12s %10s %10s %8s  %s\n",
				"QUERYID", "CALLS", "TOTAL_MS", "MEAN_MS", "SHARED_RD", "TEMP_IO", "ROWS", "QUERY")
			for _, s := range stats.Statements {
				temp := s.TempBlksRead + s.TempBlksWritten
				q := s.QueryText
				if q == "" {
					q = "(redacted)"
				}
				fmt.Fprintf(opts.Out, "%-18d %10d %12.1f %12.3f %10d %10d %8d  %s\n",
					s.QueryID, s.Calls, s.TotalExecTimeMS, s.MeanExecTimeMS, s.SharedBlksRead, temp, s.Rows, q)
			}
			for _, l := range lims {
				fmt.Fprintf(opts.Out, "limitation: [%s] %s\n", l.Area, l.Reason)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&sort, "sort", "total-time", "Sort key: total-time|mean-time|calls|shared-reads|temp-io|wal")
	cmd.Flags().IntVar(&limit, "limit", 20, "Maximum statements to return")
	return cmd
}

func newQueriesExplainCmd(opts *Options) *cobra.Command {
	var file string
	var analyze bool
	var buffers bool
	var trusted bool
	var authorizeAnalyze bool
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "Explain a read-only SQL file (ANALYZE is opt-in)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			sqlBytes, err := os.ReadFile(file)
			if err != nil {
				return err
			}
			if analyze && !authorizeAnalyze {
				return fmt.Errorf("EXPLAIN ANALYZE requires --authorize-analyze (preferably against an isolated test database)")
			}

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

			var dbname string
			_ = pool.QueryRow(ctx, `SELECT current_database()`).Scan(&dbname)

			result, err := explain.Run(ctx, pool.Pool, string(sqlBytes), explain.RunOptions{
				Analyze:         analyze,
				Buffers:         buffers || analyze,
				TrustedIsolated: trusted,
				Timeout:         cfg.StatementTimeout,
				PreviewLen:      cfg.QueryPreviewLen,
				Database:        dbname,
			})
			if err != nil {
				return err
			}

			if cfg.Format == "json" || cfg.Format == "yaml" {
				return encodeOut(opts.Out, cfg.Format, result)
			}

			mode := ""
			if analyze {
				mode = " ANALYZE"
			}
			fmt.Fprintf(opts.Out, "EXPLAIN%s summary\n", mode)
			fmt.Fprintf(opts.Out, "SQL: %s\n", result.SQLPreview)
			s := result.Summary
			fmt.Fprintf(opts.Out, "Nodes=%d seq=%d idx=%d idx_only=%d bitmap=%d nested=%d hash=%d merge=%d sort=%d agg=%d\n",
				s.NodeCount, s.SeqScans, s.IndexScans, s.IndexOnlyScans, s.BitmapScans, s.NestedLoops, s.HashJoins, s.MergeJoins, s.Sorts, s.Aggregates)
			fmt.Fprintf(opts.Out, "Root: %s cost=%.2f..%.2f rows=%.0f\n",
				result.Root.NodeType, result.Root.StartupCost, result.Root.TotalCost, result.Root.PlanRows)
			if result.PlanningTimeMS != nil {
				fmt.Fprintf(opts.Out, "Planning time: %.3f ms\n", *result.PlanningTimeMS)
			}
			if result.ExecutionTimeMS != nil {
				fmt.Fprintf(opts.Out, "Execution time: %.3f ms\n", *result.ExecutionTimeMS)
			}
			if len(result.Findings) == 0 {
				fmt.Fprintln(opts.Out, "Findings: none (no evidence-backed plan issues detected)")
			} else {
				fmt.Fprintf(opts.Out, "Findings (%d):\n", len(result.Findings))
				for _, f := range result.Findings {
					fmt.Fprintf(opts.Out, "  [%s] %s — %s\n", strings.ToUpper(string(f.Severity)), f.RuleID, f.Title)
					fmt.Fprintf(opts.Out, "    %s\n", f.Description)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "Path to SQL file containing a single read-only statement")
	cmd.Flags().BoolVar(&analyze, "analyze", false, "Run EXPLAIN ANALYZE (executes the query; opt-in)")
	cmd.Flags().BoolVar(&buffers, "buffers", false, "Include BUFFERS (implied with --analyze)")
	cmd.Flags().BoolVar(&authorizeAnalyze, "authorize-analyze", false, "Required confirmation for EXPLAIN ANALYZE")
	cmd.Flags().BoolVar(&trusted, "trusted-isolated", false, "Acknowledge a trusted isolated database for ANALYZE side-effect overrides")
	return cmd
}

func encodeOut(w io.Writer, format string, v any) error {
	switch format {
	case "yaml":
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		defer enc.Close()
		return enc.Encode(v)
	default:
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	}
}
