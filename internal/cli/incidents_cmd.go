package cli

import (
	"fmt"
	"time"

	"github.com/hghukasyan/pgscope/internal/incident"
	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/spf13/cobra"
)

func newIncidentsCmd(opts *Options) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "incidents",
		Short: "List and manage locally tracked performance incidents",
	}
	cmd.AddCommand(newIncidentsListCmd(opts))
	cmd.AddCommand(newIncidentsShowCmd(opts))
	cmd.AddCommand(newIncidentsTimelineCmd(opts))
	cmd.AddCommand(newIncidentsResolveCmd(opts))
	return cmd
}

func newIncidentsListCmd(opts *Options) *cobra.Command {
	var status string
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List incidents",
		RunE: func(cmd *cobra.Command, _ []string) error {
			store, err := openHistory(opts)
			if err != nil {
				return err
			}
			defer store.Close()
			incs, err := store.ListIncidents(cmd.Context(), models.IncidentStatus(status), limit)
			if err != nil {
				return err
			}
			return writeJSONOrText(opts, incs, func() error {
				if len(incs) == 0 {
					fmt.Fprintln(opts.Out, "No incidents.")
					return nil
				}
				for _, inc := range incs {
					fmt.Fprintf(opts.Out, "%s  %-12s %-8s  %s\n",
						inc.ID, inc.Status, inc.Severity, inc.Title)
				}
				return nil
			})
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "Filter by status (open|investigating|mitigated|resolved)")
	cmd.Flags().IntVar(&limit, "limit", 50, "Max rows")
	return cmd
}

func newIncidentsShowCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "show <incident-id>",
		Short: "Show incident investigation report",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openHistory(opts)
			if err != nil {
				return err
			}
			defer store.Close()
			eng := incident.New(store, opts.Config.Incidents, opts.Config.Correlation)
			rep, err := eng.Investigate(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return incident.WriteInvestigation(opts.Out, rep, opts.Config.Format)
		},
	}
}

func newIncidentsTimelineCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "timeline <incident-id>",
		Short: "Show observed incident timeline",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openHistory(opts)
			if err != nil {
				return err
			}
			defer store.Close()
			events, err := store.ListTimeline(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			return writeJSONOrText(opts, events, func() error {
				if len(events) == 0 {
					fmt.Fprintln(opts.Out, "No timeline events.")
					return nil
				}
				for _, e := range events {
					mark := "observed"
					if !e.Observed {
						mark = "window"
					}
					fmt.Fprintf(opts.Out, "%s  %s  [%s]\n", e.At.Format(time.RFC3339), e.Summary, mark)
				}
				return nil
			})
		},
	}
}

func newIncidentsResolveCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "resolve <incident-id>",
		Short: "Mark an incident resolved in local history (does not change PostgreSQL)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := openHistory(opts)
			if err != nil {
				return err
			}
			defer store.Close()
			eng := incident.New(store, opts.Config.Incidents, opts.Config.Correlation)
			inc, err := eng.Resolve(cmd.Context(), args[0], time.Now().UTC())
			if err != nil {
				return err
			}
			fmt.Fprintf(opts.Out, "Resolved %s (local history only)\n", inc.ID)
			return nil
		},
	}
}
