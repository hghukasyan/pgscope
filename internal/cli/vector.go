package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/internal/explain"
	"github.com/hghukasyan/pgscope/internal/history"
	"github.com/hghukasyan/pgscope/internal/vector"
	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/spf13/cobra"
)

func newVectorCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vector",
		Short: "pgvector discovery, recall, tuning, and storage intelligence",
	}
	cmd.AddCommand(newVectorInspectCmd(opts))
	cmd.AddCommand(newVectorExplainCmd(opts))
	cmd.AddCommand(newVectorRecallCmd(opts))
	cmd.AddCommand(newVectorFiltersCmd(opts))
	cmd.AddCommand(newVectorTuneCmd(opts))
	cmd.AddCommand(newVectorStorageCmd(opts))
	cmd.AddCommand(newVectorRegressionsCmd(opts))
	return cmd
}

func newVectorInspectCmd(opts *Options) *cobra.Command {
	var schema, table string
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Discover pgvector types, columns, and ANN indexes",
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

			inv, err := vector.Discover(ctx, pool.Pool, vector.DiscoverOptions{Schema: schema, Table: table})
			if err != nil {
				return err
			}
			findings := vector.AnalyzeInventory(inv, "")
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"inventory": inv, "findings": findings})
			}
			if !inv.Available {
				fmt.Fprintln(opts.Out, "pgvector: not installed")
				for _, l := range inv.Limitations {
					fmt.Fprintf(opts.Out, "  [%s] %s — %s\n", l.Area, l.Reason, l.Impact)
					if l.Hint != "" {
						fmt.Fprintf(opts.Out, "    Hint: %s\n", l.Hint)
					}
				}
				return nil
			}
			c := inv.Capabilities
			fmt.Fprintf(opts.Out, "pgvector %s (schema=%s)\n", c.ExtensionVersion, c.ExtensionSchema)
			fmt.Fprintf(opts.Out, "  types: %s\n", strings.Join(c.Types, ", "))
			fmt.Fprintf(opts.Out, "  access methods: %s (hnsw=%v ivfflat=%v)\n", strings.Join(c.AccessMethods, ", "), c.HasHNSW, c.HasIVFFlat)
			fmt.Fprintf(opts.Out, "  operators: %s\n", strings.Join(c.DistanceOperators, ", "))
			fmt.Fprintf(opts.Out, "  settings: %s\n", strings.Join(c.SettingsAvailable, ", "))
			fmt.Fprintf(opts.Out, "Columns (%d):\n", len(inv.Columns))
			for _, col := range inv.Columns {
				dims := "unconstrained"
				if col.Dimensions != nil {
					dims = fmt.Sprintf("%d", *col.Dimensions)
				}
				fmt.Fprintf(opts.Out, "  - %s.%s.%s type=%s dims=%s rows≈%d indexed=%v\n",
					col.Schema, col.Table, col.Column, col.TypeName, dims, col.EstimatedRows, col.HasVectorIndex)
			}
			fmt.Fprintf(opts.Out, "Indexes (%d):\n", len(inv.Indexes))
			for _, idx := range inv.Indexes {
				fmt.Fprintf(opts.Out, "  - %s.%s am=%s opc=%s metric=%s size=%s scans=%d valid=%t opts=%v\n",
					idx.Schema, idx.Name, idx.AccessMethod, idx.OperatorClass, idx.DistanceMetric, idx.SizePretty, idx.IndexScans, idx.IsValid, idx.Options)
			}
			if len(findings) > 0 {
				fmt.Fprintf(opts.Out, "Findings (%d):\n", len(findings))
				for _, f := range findings {
					fmt.Fprintf(opts.Out, "  [%s] %s — %s\n", strings.ToUpper(string(f.Severity)), f.RuleID, f.Title)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&schema, "schema", "", "Limit to schema")
	cmd.Flags().StringVar(&table, "table", "", "Limit to table name")
	return cmd
}

func newVectorExplainCmd(opts *Options) *cobra.Command {
	var file string
	cmd := &cobra.Command{
		Use:   "explain",
		Short: "EXPLAIN a vector SQL file with vector-specific diagnostics",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if file == "" {
				return fmt.Errorf("--file is required")
			}
			sqlBytes, err := os.ReadFile(file)
			if err != nil {
				return err
			}
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
			var dbname string
			_ = pool.QueryRow(ctx, `SELECT current_database()`).Scan(&dbname)
			res, err := explain.Run(ctx, pool.Pool, string(sqlBytes), explain.RunOptions{
				Timeout: cfg.StatementTimeout, PreviewLen: cfg.QueryPreviewLen, Database: dbname,
			})
			if err != nil {
				return err
			}
			inv, _ := vector.Discover(ctx, pool.Pool, vector.DiscoverOptions{})
			vector.AnalyzeVectorPlan(&res, inv, dbname)
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, res)
			}
			fmt.Fprintf(opts.Out, "Vector EXPLAIN\nSQL: %s\n", res.SQLPreview)
			fmt.Fprintf(opts.Out, "Root: %s cost=%.2f..%.2f rows=%.0f\n",
				res.Root.NodeType, res.Root.StartupCost, res.Root.TotalCost, res.Root.PlanRows)
			fmt.Fprintf(opts.Out, "Findings (%d):\n", len(res.Findings))
			for _, f := range res.Findings {
				fmt.Fprintf(opts.Out, "  [%s] %s — %s\n", strings.ToUpper(string(f.Severity)), f.RuleID, f.Title)
				fmt.Fprintf(opts.Out, "    %s\n", f.Description)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&file, "file", "", "SQL file (single read-only statement)")
	return cmd
}

func newVectorRecallCmd(opts *Options) *cobra.Command {
	var configPath string
	var authorize bool
	var historyDir string
	var persistHistory bool
	cmd := &cobra.Command{
		Use:   "recall",
		Short: "Opt-in Recall@K benchmark (isolated/lab)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return fmt.Errorf("--config workload.yaml is required")
			}
			w, err := vector.LoadWorkload(configPath)
			if err != nil {
				return err
			}
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), w.Benchmark.MaxDuration+cfg.Timeout)
			defer cancel()
			pool, err := database.Connect(ctx, cfg)
			if err != nil {
				return err
			}
			defer pool.Close()
			rep, err := vector.RunRecall(ctx, pool.Pool, w, vector.RecallOptions{Authorize: authorize})
			if err != nil {
				return err
			}
			if historyDir != "" {
				if path, err := vector.SaveRecallHistory(historyDir, rep); err == nil {
					fmt.Fprintf(opts.Err, "saved history: %s\n", path)
				}
			}
			if persistHistory || cfg.History.Enabled {
				store, err := openHistory(opts)
				if err != nil {
					fmt.Fprintf(opts.Err, "warning: history persist skipped: %v\n", err)
				} else {
					defer store.Close()
					ik := rep.Database
					if err := history.PersistRecallReport(ctx, store, ik, rep); err != nil {
						fmt.Fprintf(opts.Err, "warning: history persist failed: %v\n", err)
					} else {
						fmt.Fprintf(opts.Err, "persisted vector benchmark summaries to local history\n")
					}
				}
			}
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, rep)
			}
			printRecallReport(opts, rep)
			return exitFromFindings(cmd, rep.Findings)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Workload YAML path")
	cmd.Flags().BoolVar(&authorize, "authorize-benchmark", false, "Required confirmation for recall benchmarking")
	cmd.Flags().StringVar(&historyDir, "history-dir", "", "Optional directory to store compact recall history JSON")
	cmd.Flags().BoolVar(&persistHistory, "persist-history", false, "Persist compact recall summaries to local SQLite history")
	return cmd
}

func newVectorFiltersCmd(opts *Options) *cobra.Command {
	parent := &cobra.Command{Use: "filters", Short: "Filtered ANN diagnostics"}
	var configPath string
	var authorize bool
	analyze := &cobra.Command{
		Use:   "analyze",
		Short: "Per-filter-cohort recall analysis",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return fmt.Errorf("--config required")
			}
			w, err := vector.LoadWorkload(configPath)
			if err != nil {
				return err
			}
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), w.Benchmark.MaxDuration+cfg.Timeout)
			defer cancel()
			pool, err := database.Connect(ctx, cfg)
			if err != nil {
				return err
			}
			defer pool.Close()
			rep, err := vector.AnalyzeFilters(ctx, pool.Pool, w, authorize)
			if err != nil {
				return err
			}
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, rep)
			}
			printRecallReport(opts, rep)
			return exitFromFindings(cmd, rep.Findings)
		},
	}
	analyze.Flags().StringVar(&configPath, "config", "", "Workload YAML with search.filter")
	analyze.Flags().BoolVar(&authorize, "authorize-benchmark", false, "Required confirmation")
	parent.AddCommand(analyze)
	return parent
}

func newVectorTuneCmd(opts *Options) *cobra.Command {
	var configPath, objective string
	var authorize bool
	cmd := &cobra.Command{
		Use:   "tune",
		Short: "Bounded HNSW/IVFFlat search-parameter experiments",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return fmt.Errorf("--config required")
			}
			w, err := vector.LoadWorkload(configPath)
			if err != nil {
				return err
			}
			cfg, err := opts.withDSN()
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), w.Benchmark.MaxDuration+cfg.Timeout)
			defer cancel()
			pool, err := database.Connect(ctx, cfg)
			if err != nil {
				return err
			}
			defer pool.Close()
			rep, err := vector.RunTune(ctx, pool.Pool, w, vector.TuneOptions{Authorize: authorize, Objective: objective})
			if err != nil {
				return err
			}
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, rep)
			}
			fmt.Fprintf(opts.Out, "Vector tune (%s) target=%s am=%s\n", rep.Objective, rep.Target, rep.AccessMethod)
			if rep.Baseline != nil {
				fmt.Fprintf(opts.Out, "Baseline: meets=%v summaries=%d\n", rep.Baseline.MeetsObjectives, len(rep.Baseline.Summaries))
			}
			for _, c := range rep.Candidates {
				fmt.Fprintf(opts.Out, "  candidate %v meets=%v err=%q\n", c.Settings, c.MeetsObjectives, c.Error)
			}
			fmt.Fprintf(opts.Out, "Recommendation: %s\n", rep.Recommendation)
			if rep.ParetoNotes != "" {
				fmt.Fprintf(opts.Out, "Notes: %s\n", rep.ParetoNotes)
			}
			return exitFromFindings(cmd, rep.Findings)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "Workload YAML")
	cmd.Flags().StringVar(&objective, "objective", "balanced", "recall|latency|balanced")
	cmd.Flags().BoolVar(&authorize, "authorize-benchmark", false, "Required confirmation")
	return cmd
}

func newVectorStorageCmd(opts *Options) *cobra.Command {
	var schema, table string
	cmd := &cobra.Command{
		Use:   "storage",
		Short: "Vector storage and capacity estimates",
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
			inv, err := vector.Discover(ctx, pool.Pool, vector.DiscoverOptions{Schema: schema, Table: table})
			if err != nil {
				return err
			}
			rep := vector.EstimateStorage(inv)
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, rep)
			}
			fmt.Fprintln(opts.Out, "Vector storage (estimates labeled; measured sizes from PostgreSQL)")
			fmt.Fprintf(opts.Out, "%s\n", rep.Notes)
			for _, e := range rep.Estimates {
				dims := "n/a"
				if e.Dimensions != nil {
					dims = fmt.Sprintf("%d", *e.Dimensions)
				}
				fmt.Fprintf(opts.Out, "  - %s.%s.%s type=%s dims=%s rows≈%d\n", e.Schema, e.Table, e.Column, e.TypeName, dims, e.EstimatedRows)
				fmt.Fprintf(opts.Out, "      measured table=%s index=%s\n", vector.FormatStorageBytes(e.MeasuredTableBytes), vector.FormatStorageBytes(e.MeasuredIndexBytes))
				fmt.Fprintf(opts.Out, "      estimated vector_payload=%s halfvec_payload=%s\n",
					vector.FormatStorageBytes(e.EstVectorPayloadB), vector.FormatStorageBytes(e.EstHalfvecPayloadB))
				fmt.Fprintf(opts.Out, "      note: %s\n", e.Notes)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&schema, "schema", "", "Limit schema")
	cmd.Flags().StringVar(&table, "table", "", "Limit table")
	return cmd
}

func newVectorRegressionsCmd(opts *Options) *cobra.Command {
	var baselinePath, currentPath string
	cmd := &cobra.Command{
		Use:   "regressions",
		Short: "Compare compatible recall history reports",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if baselinePath == "" || currentPath == "" {
				return fmt.Errorf("--baseline and --current report paths are required")
			}
			b, err := vector.LoadRecallReport(baselinePath)
			if err != nil {
				return err
			}
			c, err := vector.LoadRecallReport(currentPath)
			if err != nil {
				return err
			}
			cmp := vector.CompareRecallReports(b, c)
			cfg := opts.Config
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, cmp)
			}
			fmt.Fprintf(opts.Out, "Compatible: %v\n", cmp.Compatible)
			if cmp.Reason != "" {
				fmt.Fprintf(opts.Out, "Reason: %s\n", cmp.Reason)
			}
			for _, f := range cmp.Findings {
				fmt.Fprintf(opts.Out, "  [%s] %s — %s\n", strings.ToUpper(string(f.Severity)), f.RuleID, f.Title)
				fmt.Fprintf(opts.Out, "    %s\n", f.Description)
			}
			return exitFromFindings(cmd, cmp.Findings)
		},
	}
	cmd.Flags().StringVar(&baselinePath, "baseline", "", "Baseline recall JSON")
	cmd.Flags().StringVar(&currentPath, "current", "", "Current recall JSON")
	return cmd
}

func printRecallReport(opts *Options, rep models.RecallReport) {
	fmt.Fprintf(opts.Out, "PGScope — Vector Recall\n")
	fmt.Fprintf(opts.Out, "Target: %s  metric=%s  index=%s (%s)\n", rep.Target, rep.Metric, rep.IndexName, rep.AccessMethod)
	fmt.Fprintf(opts.Out, "pgvector=%s  fingerprint=%s  env=%s  baseline_verified=%v\n",
		rep.PgvectorVersion, rep.WorkloadFingerprint, rep.Environment, rep.ExactBaselineVerified)
	for _, s := range rep.Summaries {
		fmt.Fprintf(opts.Out, "Recall@%d: mean=%.3f min=%.3f samples=%d incomplete=%d errors=%d\n",
			s.K, s.MeanRecall, s.MinRecall, s.Samples, s.IncompleteResults, s.Errors)
		warn := s.ApproxLatency.P99Warning
		if warn != "" {
			warn = " (" + warn + ")"
		}
		fmt.Fprintf(opts.Out, "  approx latency p50=%.1f p95=%.1f p99=%.1f ms%s\n",
			s.ApproxLatency.P50MS, s.ApproxLatency.P95MS, s.ApproxLatency.P99MS, warn)
	}
	for _, g := range rep.FilterSummaries {
		fmt.Fprintf(opts.Out, "  filter %q K=%d mean=%.3f min=%.3f samples=%d incomplete=%d\n",
			g.Label, g.K, g.MeanRecall, g.MinRecall, g.Samples, g.Incomplete)
	}
	for _, f := range rep.Findings {
		fmt.Fprintf(opts.Out, "Finding [%s] %s — %s\n", strings.ToUpper(string(f.Severity)), f.RuleID, f.Title)
	}
}

func exitFromFindings(cmd *cobra.Command, findings []models.Finding) error {
	status := "ok"
	for _, f := range findings {
		switch f.Severity {
		case models.SeverityCritical:
			status = "critical"
		case models.SeverityWarning:
			if status != "critical" {
				status = "warning"
			}
		}
	}
	code := exitCodeForStatus(status)
	if code != 0 {
		return &exitError{code: code}
	}
	return nil
}
