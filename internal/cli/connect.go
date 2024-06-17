package cli

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/spf13/cobra"
)

func newConnectCmd(opts *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "connect",
		Short: "Verify connectivity and print instance identity",
		RunE: func(cmd *cobra.Command, _ []string) error {
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

			col := collector.New(pool, cfg, opts.Logger)
			snap, err := col.Collect(ctx)
			if err != nil {
				return err
			}
			inst := snap.Instance

			switch cfg.Format {
			case "json":
				return json.NewEncoder(opts.Out).Encode(map[string]any{
					"ok":       true,
					"dsn":      database.RedactDSN(cfg.DSN),
					"instance": inst,
				})
			default:
				fmt.Fprintf(opts.Out, "Connected successfully\n")
				fmt.Fprintf(opts.Out, "  DSN:      %s\n", database.RedactDSN(cfg.DSN))
				fmt.Fprintf(opts.Out, "  Version:  %s\n", collector.FormatVersionShort(inst))
				fmt.Fprintf(opts.Out, "  Database: %s\n", inst.Database)
				fmt.Fprintf(opts.Out, "  User:     %s\n", inst.User)
				fmt.Fprintf(opts.Out, "  Recovery: %v\n", inst.IsInRecovery)
				return nil
			}
		},
	}
}
