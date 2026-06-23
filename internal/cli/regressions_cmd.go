package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/internal/regression"
	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/spf13/cobra"
)

func newRegressionsCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "regressions",
		Short: "List or detect performance regressions from history",
	}
	cmd.AddCommand(newRegressionsListCmd(opts))
	cmd.AddCommand(newRegressionsDetectCmd(opts))
	cmd.AddCommand(newRegressionsQueriesCmd(opts))
	cmd.AddCommand(newRegressionsVectorCmd(opts))
	return cmd
}

func newRegressionsListCmd(opts *Options) *cobra.Command {
	var last time.Duration
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List stored regressions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openHistory(opts)
			if err != nil {
				return err
			}
			defer store.Close()
			since := time.Now().UTC().Add(-last)
			regs, err := store.ListRegressions(cmd.Context(), since, time.Time{}, limit)
			if err != nil {
				return err
			}
			return writeJSONOrText(opts, regs, func() error {
				if len(regs) == 0 {
					fmt.Fprintln(opts.Out, "No regressions.")
					return nil
				}
				for _, r := range regs {
					fmt.Fprintf(opts.Out, "%s  %s  %+.0f%%  sev=%s rel=%s\n",
						r.DetectedAt.Format(time.RFC3339), r.MetricName, r.RelativeChange*100, r.Severity, r.Reliability)
				}
				return nil
			})
		},
	}
	cmd.Flags().DurationVar(&last, "last", 24*time.Hour, "Lookback")
	cmd.Flags().IntVar(&limit, "limit", 100, "Max rows")
	return cmd
}

func newRegressionsDetectCmd(opts *Options) *cobra.Command {
	var last time.Duration
	cmd := &cobra.Command{
		Use:   "detect",
		Short: "Run regression detection over recent history",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return detectPrefix(opts, cmd, last, "")
		},
	}
	cmd.Flags().DurationVar(&last, "last", time.Hour, "History window to analyze")
	return cmd
}

func newRegressionsQueriesCmd(opts *Options) *cobra.Command {
	var last time.Duration
	cmd := &cobra.Command{
		Use:   "queries",
		Short: "Detect query-related regressions only",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return detectPrefix(opts, cmd, last, "query.")
		},
	}
	cmd.Flags().DurationVar(&last, "last", time.Hour, "History window")
	return cmd
}

func newRegressionsVectorCmd(opts *Options) *cobra.Command {
	var last time.Duration
	cmd := &cobra.Command{
		Use:   "vector",
		Short: "Detect pgvector benchmark regressions only",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return detectPrefix(opts, cmd, last, "vector.")
		},
	}
	cmd.Flags().DurationVar(&last, "last", 7*24*time.Hour, "History window")
	return cmd
}

func detectPrefix(opts *Options, cmd *cobra.Command, last time.Duration, prefix string) error {
	store, err := openHistory(opts)
	if err != nil {
		return err
	}
	defer store.Close()
	since := time.Now().UTC().Add(-last)
	samples, err := store.ListMetrics(cmd.Context(), "", since, time.Time{}, 20000)
	if err != nil {
		return err
	}
	var selected []models.MetricSample
	if prefix == "" {
		selected = samples
	} else {
		for _, s := range samples {
			if strings.HasPrefix(s.Name, prefix) {
				selected = append(selected, s)
			}
		}
	}
	det := regression.New(opts.Config.Regression)
	regs := det.DetectGroups(selected, time.Now().UTC())
	if err := store.SaveRegressions(cmd.Context(), regs); err != nil {
		return err
	}
	return writeJSONOrText(opts, regs, func() error {
		if len(regs) == 0 {
			fmt.Fprintln(opts.Out, "No regressions detected.")
			return nil
		}
		for _, r := range regs {
			fmt.Fprintf(opts.Out, "%s  %+.0f%%  (baseline=%.4g current=%.4g) [%s]\n",
				r.MetricName, r.RelativeChange*100, r.BaselineValue, r.CurrentValue, r.Reliability)
		}
		return nil
	})
}
