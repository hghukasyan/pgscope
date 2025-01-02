package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/internal/diagnosis"
	"github.com/hghukasyan/pgscope/internal/rules"
	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/spf13/cobra"
)

func newIndexesCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "indexes",
		Short: "Index intelligence",
	}
	cmd.AddCommand(newIndexesInspectCmd(opts))
	cmd.AddCommand(newIndexesUnusedCmd(opts))
	cmd.AddCommand(newIndexesDuplicatesCmd(opts))
	cmd.AddCommand(newIndexesRecommendCmd(opts))
	return cmd
}

func newIndexesInspectCmd(opts *Options) *cobra.Command {
	var table string
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect index inventory and usage",
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
			stats, lims := col.CollectIndexes(ctx, table)
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"indexes": stats, "limitations": lims})
			}
			fmt.Fprintf(opts.Out, "Indexes: %d  invalid=%d  zero-scan candidates=%d  duplicates=%d  redundant=%d\n",
				len(stats.Indexes), len(stats.Invalid), len(stats.NeverUsed), len(stats.Duplicates), len(stats.Redundant))
			for _, idx := range stats.Largest {
				fmt.Fprintf(opts.Out, "  - %s.%s on %s.%s am=%s size=%s scans=%d valid=%t %s\n",
					idx.Schema, idx.Name, idx.TableSchema, idx.TableName, idx.AccessMethod, idx.SizePretty, idx.IndexScans, idx.IsValid, shortDef(idx.Definition))
			}
			printLims(opts, lims)
			return nil
		},
	}
	cmd.Flags().StringVar(&table, "table", "", "Limit to schema.table")
	return cmd
}

func newIndexesUnusedCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "unused",
		Short: "List zero-scan index candidates (not drop recommendations)",
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
			stats, lims := col.CollectIndexes(ctx, "")
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"never_used_candidates": stats.NeverUsed, "stats_reset": stats.StatsReset, "limitations": lims})
			}
			fmt.Fprintln(opts.Out, "Zero-scan candidates (non-unique, non-constraint). Not automatic drop advice:")
			for _, idx := range stats.NeverUsed {
				fmt.Fprintf(opts.Out, "  - %s.%s on %s.%s size=%s am=%s\n", idx.Schema, idx.Name, idx.TableSchema, idx.TableName, idx.SizePretty, idx.AccessMethod)
			}
			if stats.StatsReset != nil {
				fmt.Fprintf(opts.Out, "stats_reset: %s\n", stats.StatsReset.UTC().Format("2006-01-02T15:04:05Z"))
			}
			printLims(opts, lims)
			return nil
		},
	}
}

func newIndexesDuplicatesCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "duplicates",
		Short: "Show structurally identical / redundant index groups",
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
			stats, lims := col.CollectIndexes(ctx, "")
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"duplicates": stats.Duplicates, "redundant": stats.Redundant, "limitations": lims})
			}
			fmt.Fprintf(opts.Out, "Duplicate groups: %d\n", len(stats.Duplicates))
			for _, g := range stats.Duplicates {
				fmt.Fprintf(opts.Out, "  %s.%s: %s\n", g.TableSchema, g.TableName, g.Reason)
				for _, idx := range g.Indexes {
					fmt.Fprintf(opts.Out, "    - %s.%s\n", idx.Schema, idx.Name)
				}
			}
			fmt.Fprintf(opts.Out, "Redundant B-tree candidates: %d\n", len(stats.Redundant))
			for _, g := range stats.Redundant {
				fmt.Fprintf(opts.Out, "  - %s\n", g.Reason)
			}
			printLims(opts, lims)
			return nil
		},
	}
}

func newIndexesRecommendCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "recommend",
		Short: "Show conservative missing-index candidates from scan evidence",
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
			snap, err := col.Collect(ctx)
			if err != nil {
				return err
			}
			eng := diagnosis.New(rules.DuplicateIndexRule{}, rules.RedundantBTreeIndexRule{}, rules.UnusedIndexObservationRule{}, rules.LargeLowUsageIndexRule{}, rules.InvalidIndexRule{})
			result, err := eng.Analyze(ctx, snap)
			if err != nil {
				return err
			}
			cands := collector.RecommendIndexesFromPlans(result.Findings, snap.Indexes.Indexes)
			snap.Indexes.Candidates = cands
			if cfg.Format != "text" {
				return encodeOut(opts.Out, cfg.Format, map[string]any{"candidates": cands, "findings": result.Findings, "limitations": snap.Limitations})
			}
			if len(cands) == 0 {
				fmt.Fprintln(opts.Out, "No high-confidence missing-index candidates from current evidence.")
				fmt.Fprintln(opts.Out, "Use `pgscope queries explain --file ...` to generate plan-backed candidates.")
			}
			for _, c := range cands {
				fmt.Fprintf(opts.Out, "Candidate: %s.%s (%s)\n", c.Schema, c.Table, strings.Join(c.Columns, ", "))
				fmt.Fprintf(opts.Out, "  SQL: %s\n", c.CreateSQL)
				fmt.Fprintf(opts.Out, "  Why: %s\n", c.Reason)
				fmt.Fprintf(opts.Out, "  Write overhead: %s\n", c.WriteOverhead)
				fmt.Fprintf(opts.Out, "  Validation: %s\n", c.Validation)
			}
			return nil
		},
	}
}

func shortDef(def string) string {
	def = strings.ReplaceAll(def, "\n", " ")
	if len(def) > 80 {
		return def[:77] + "..."
	}
	return def
}

func printLims(opts *Options, lims []models.Limitation) {
	for _, l := range lims {
		fmt.Fprintf(opts.Out, "limitation: [%s] %s\n", l.Area, l.Reason)
	}
}
