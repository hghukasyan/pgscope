package cli

import (
	"fmt"
	"time"

	"github.com/hghukasyan/pgscope/internal/incident"
	"github.com/hghukasyan/pgscope/internal/regression"
	"github.com/spf13/cobra"
)

func newInvestigateCmd(opts *Options) *cobra.Command {
	var last time.Duration
	var queryID string
	var incidentID string
	cmd := &cobra.Command{
		Use:   "investigate",
		Short: "Reconstruct regressions, correlate evidence, and explain root-cause hypotheses",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openHistory(opts)
			if err != nil {
				return err
			}
			defer store.Close()

			if incidentID != "" {
				eng := incident.New(store, opts.Config.Incidents, opts.Config.Correlation)
				rep, err := eng.Investigate(cmd.Context(), incidentID)
				if err != nil {
					return err
				}
				return incident.WriteInvestigation(opts.Out, rep, opts.Config.Format)
			}

			since := time.Now().UTC().Add(-last)
			samples, err := store.ListMetrics(cmd.Context(), "", since, time.Time{}, 20000)
			if err != nil {
				return err
			}
			if queryID != "" {
				filtered := samples[:0]
				for _, s := range samples {
					if s.Dimensions["query_id"] == queryID {
						filtered = append(filtered, s)
					}
				}
				samples = filtered
			}

			det := regression.New(opts.Config.Regression)
			regs := det.DetectGroups(samples, time.Now().UTC())
			if err := store.SaveRegressions(cmd.Context(), regs); err != nil {
				return err
			}

			eng := incident.New(store, opts.Config.Incidents, opts.Config.Correlation)
			incs, err := eng.ProcessRegressions(cmd.Context(), regs, nil, time.Now().UTC())
			if err != nil {
				return err
			}
			if len(incs) == 0 {
				fmt.Fprintln(opts.Out, "No incidents formed from current regressions.")
				if len(regs) > 0 {
					fmt.Fprintf(opts.Out, "Detected %d regression(s) that did not open new incidents (may have been deduplicated).\n", len(regs))
				}
				return nil
			}
			// Report the most recent / first incident.
			rep, err := eng.Investigate(cmd.Context(), incs[0].ID)
			if err != nil {
				return err
			}
			return incident.WriteInvestigation(opts.Out, rep, opts.Config.Format)
		},
	}
	cmd.Flags().DurationVar(&last, "last", time.Hour, "Investigation lookback")
	cmd.Flags().StringVar(&queryID, "query-id", "", "Focus on a query fingerprint / query_id")
	cmd.Flags().StringVar(&incidentID, "incident", "", "Investigate a specific incident id")
	return cmd
}
