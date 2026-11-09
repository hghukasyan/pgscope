// Package report renders ScanResult output for humans and machines.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/pkg/models"
	"gopkg.in/yaml.v3"
)

// Write renders result in the requested format to w.
func Write(w io.Writer, result models.ScanResult, format string, color bool) error {
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	case "yaml":
		enc := yaml.NewEncoder(w)
		enc.SetIndent(2)
		defer enc.Close()
		return enc.Encode(result)
	case "text", "":
		return writeText(w, result, color)
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
}

func writeText(w io.Writer, result models.ScanResult, color bool) error {
	s := result.Snapshot
	c := colors(color)

	fmt.Fprintf(w, "%sPGScope Scan%s\n", c.bold, c.reset)
	fmt.Fprintf(w, "Status:  %s%s%s\n", statusColor(c, result.OverallStatus), strings.ToUpper(result.OverallStatus), c.reset)
	fmt.Fprintf(w, "Summary: %s\n", result.Summary)
	fmt.Fprintf(w, "Collected: %s (took %d ms)\n\n", s.CollectedAt.Format(time.RFC3339), s.DurationMS)

	fmt.Fprintf(w, "%sInstance%s\n", c.bold, c.reset)
	fmt.Fprintf(w, "  Version:    %s\n", collector.FormatVersionShort(s.Instance))
	fmt.Fprintf(w, "  Database:   %s\n", s.Instance.Database)
	fmt.Fprintf(w, "  User:       %s\n", s.Instance.User)
	fmt.Fprintf(w, "  Recovery:   %v\n", s.Instance.IsInRecovery)
	if len(s.Instance.CloudHints) > 0 {
		fmt.Fprintf(w, "  Cloud:      %s\n", strings.Join(s.Instance.CloudHints, ", "))
	}
	if len(s.Instance.Extensions) > 0 {
		exts := make([]string, 0, len(s.Instance.Extensions))
		for _, e := range s.Instance.Extensions {
			exts = append(exts, fmt.Sprintf("%s@%s", e.Name, e.Version))
		}
		fmt.Fprintf(w, "  Extensions: %s\n", strings.Join(exts, ", "))
	} else {
		fmt.Fprintf(w, "  Extensions: (none detected or unavailable)\n")
	}
	caps := s.Instance.Capabilities
	fmt.Fprintf(w, "  Caps:       pg_stat_statements=%v pgvector=%v hypopg=%v\n\n",
		caps.HasPgStatStatementsView || caps.HasPgStatStatements, caps.HasPgVector, caps.HasHypoPG)

	if s.Connections.Collected {
		fmt.Fprintf(w, "%sConnections%s\n", c.bold, c.reset)
		fmt.Fprintf(w, "  Total: %d / %d (%.1f%%)  active=%d idle=%d idle_in_xact=%d waiting_locks=%d\n",
			s.Connections.Total, s.Connections.MaxConnections, s.Connections.UtilizationPct,
			s.Connections.Active, s.Connections.Idle, s.Connections.IdleInTransaction, s.Connections.Waiting)
		if len(s.Connections.LongRunningSessions) > 0 {
			fmt.Fprintf(w, "  Long-running active sessions:\n")
			for _, sess := range s.Connections.LongRunningSessions {
				fmt.Fprintf(w, "    - pid=%d dur=%s app=%q q=%q\n", sess.PID, sess.Duration, sess.ApplicationName, sess.QueryPreview)
			}
		}
		fmt.Fprintln(w)
	}

	if s.Transactions.Collected {
		fmt.Fprintf(w, "%sTransactions%s\n", c.bold, c.reset)
		fmt.Fprintf(w, "  Long-running: %d  idle-in-xact (threshold): %d  datfrozenxid age: %d\n",
			len(s.Transactions.LongRunning), len(s.Transactions.IdleInTransaction), s.Transactions.DatfrozenxidAge)
		if s.Transactions.OldestXactAge != "" {
			fmt.Fprintf(w, "  Oldest xact age: %s\n", s.Transactions.OldestXactAge)
		}
		fmt.Fprintln(w)
	}

	if s.Locks.Collected {
		fmt.Fprintf(w, "%sLocks%s\n", c.bold, c.reset)
		fmt.Fprintf(w, "  Waiting: %d  blocking chains: %d  deadlocks(counter): %d\n",
			s.Locks.WaitingCount, len(s.Locks.BlockingChains), s.Locks.Deadlocks)
		for _, chain := range s.Locks.BlockingChains {
			fmt.Fprintf(w, "    - blocked=%d <- blocking=%d rel=%s mode=%s dur=%s\n",
				chain.BlockedPID, chain.BlockingPID, chain.Relation, chain.Mode, chain.BlockedDuration)
		}
		fmt.Fprintln(w)
	}

	if s.Storage.Collected {
		fmt.Fprintf(w, "%sStorage%s\n", c.bold, c.reset)
		fmt.Fprintf(w, "  Database size: %s\n", s.Storage.DatabaseSizePretty)
		if len(s.Storage.TopRelations) > 0 {
			fmt.Fprintf(w, "  Top relations:\n")
			for _, r := range s.Storage.TopRelations {
				fmt.Fprintf(w, "    - %s.%s (%s) %s\n", r.Schema, r.Name, r.Kind, r.Pretty)
			}
		}
		fmt.Fprintln(w)
	}

	if s.Indexes.Collected {
		fmt.Fprintf(w, "%sIndexes%s\n", c.bold, c.reset)
		fmt.Fprintf(w, "  Inventoried: %d  invalid: %d  zero-scan candidates: %d  duplicates: %d\n",
			len(s.Indexes.Indexes), len(s.Indexes.Invalid), len(s.Indexes.NeverUsed), len(s.Indexes.Duplicates))
		if len(s.Indexes.Largest) > 0 {
			fmt.Fprintf(w, "  Largest:\n")
			maxShow := 5
			if len(s.Indexes.Largest) < maxShow {
				maxShow = len(s.Indexes.Largest)
			}
			for _, idx := range s.Indexes.Largest[:maxShow] {
				fmt.Fprintf(w, "    - %s.%s on %s.%s %s scans=%d am=%s\n",
					idx.Schema, idx.Name, idx.TableSchema, idx.TableName, idx.SizePretty, idx.IndexScans, idx.AccessMethod)
			}
		}
		fmt.Fprintln(w)
	}

	if s.Queries.Collected {
		fmt.Fprintf(w, "%sQueries%s\n", c.bold, c.reset)
		fmt.Fprintf(w, "  Top statements: %d (sort=%s)\n", len(s.Queries.Statements), s.Queries.SortBy)
		maxShow := 5
		if len(s.Queries.Statements) < maxShow {
			maxShow = len(s.Queries.Statements)
		}
		for _, q := range s.Queries.Statements[:maxShow] {
			preview := q.QueryText
			if preview == "" {
				preview = "(redacted)"
			}
			fmt.Fprintf(w, "    - id=%d calls=%d total=%.1fms mean=%.3fms %s\n",
				q.QueryID, q.Calls, q.TotalExecTimeMS, q.MeanExecTimeMS, preview)
		}
		fmt.Fprintln(w)
	} else if s.Queries.Available == false && len(result.Snapshot.Limitations) > 0 {
		// limitations section covers missing extension
	}

	if s.Vacuum.Collected {
		fmt.Fprintf(w, "%sVacuum%s\n", c.bold, c.reset)
		fmt.Fprintf(w, "  Tables sampled: %d  risk tables: %d  active vacuums: %d\n",
			len(s.Vacuum.Tables), len(s.Vacuum.Risks), len(s.Vacuum.Progress))
		fmt.Fprintln(w)
	}

	if s.WAL.Collected {
		fmt.Fprintf(w, "%sWAL / Checkpoints%s\n", c.bold, c.reset)
		if s.WAL.WALBytes != nil {
			fmt.Fprintf(w, "  wal_bytes: %d\n", *s.WAL.WALBytes)
		}
		if s.WAL.CheckpointsReq != nil && s.WAL.CheckpointsTimed != nil {
			fmt.Fprintf(w, "  checkpoints: timed=%d requested=%d\n", *s.WAL.CheckpointsTimed, *s.WAL.CheckpointsReq)
		}
		fmt.Fprintln(w)
	}

	if s.Replication.Collected {
		fmt.Fprintf(w, "%sReplication%s\n", c.bold, c.reset)
		fmt.Fprintf(w, "  Role: %s  connections: %d  slots: %d\n",
			s.Replication.Role, len(s.Replication.Connections), len(s.Replication.Slots))
		fmt.Fprintln(w)
	}

	if s.Vector.Collected {
		fmt.Fprintf(w, "%sVector (pgvector)%s\n", c.bold, c.reset)
		if !s.Vector.Available {
			fmt.Fprintf(w, "  Available: false\n")
		} else {
			fmt.Fprintf(w, "  Version: %s  columns: %d  ann_indexes: %d  hnsw=%v ivfflat=%v\n",
				s.Vector.Capabilities.ExtensionVersion, len(s.Vector.Columns), len(s.Vector.Indexes),
				s.Vector.Capabilities.HasHNSW, s.Vector.Capabilities.HasIVFFlat)
			settings := s.Vector.Settings
			if settings == nil {
				settings = map[string]string{}
			}
			sb, ec := settings["shared_buffers"], settings["effective_cache_size"]
			if sb == "" && s.Instance.Settings != nil {
				sb = s.Instance.Settings["shared_buffers"]
			}
			if ec == "" && s.Instance.Settings != nil {
				ec = s.Instance.Settings["effective_cache_size"]
			}
			if sb != "" || ec != "" {
				fmt.Fprintf(w, "  Memory: shared_buffers=%s  effective_cache_size=%s\n",
					fallbackDash(sb), fallbackDash(ec))
			}
			for _, idx := range s.Vector.Indexes {
				if idx.AccessMethod != "hnsw" || idx.SizeBytes <= 0 {
					continue
				}
				fmt.Fprintf(w, "  HNSW %s.%s size=%s", idx.Schema, idx.Name, idx.SizePretty)
				if sb != "" {
					fmt.Fprintf(w, "  vs shared_buffers=%s", sb)
				}
				if ec != "" {
					fmt.Fprintf(w, "  vs effective_cache_size=%s", ec)
				}
				fmt.Fprintln(w)
			}
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "%sFindings (%d)%s\n", c.bold, len(result.Findings), c.reset)
	if len(result.Findings) == 0 {
		fmt.Fprintf(w, "  No problems detected from available evidence.\n\n")
	} else {
		for _, f := range result.Findings {
			fmt.Fprintf(w, "  [%s%s%s] %s — %s\n", severityColor(c, f.Severity), strings.ToUpper(string(f.Severity)), c.reset, f.RuleID, f.Title)
			fmt.Fprintf(w, "    %s\n", f.Description)
			if f.ProbableCause != "" {
				fmt.Fprintf(w, "    Probable cause: %s\n", f.ProbableCause)
			}
			if f.Remediation != "" {
				fmt.Fprintf(w, "    Remediation: %s\n", f.Remediation)
			}
			if len(f.Evidence) > 0 {
				fmt.Fprintf(w, "    Evidence:\n")
				for _, e := range f.Evidence {
					if e.Value != "" {
						fmt.Fprintf(w, "      - [%s] %s = %s\n", e.Kind, e.Description, e.Value)
					} else {
						fmt.Fprintf(w, "      - [%s] %s\n", e.Kind, e.Description)
					}
				}
			}
			fmt.Fprintln(w)
		}
	}

	if len(result.Recommendations) > 0 {
		fmt.Fprintf(w, "%sRecommendations%s\n", c.bold, c.reset)
		for _, r := range result.Recommendations {
			fmt.Fprintf(w, "  - [%s] %s: %s\n", r.Risk, r.Title, r.Summary)
		}
		fmt.Fprintln(w)
	}

	if len(s.Limitations) > 0 {
		fmt.Fprintf(w, "%sCollection limitations%s\n", c.bold, c.reset)
		for _, l := range s.Limitations {
			fmt.Fprintf(w, "  - [%s] %s — %s\n", l.Area, l.Reason, l.Impact)
			if l.Hint != "" {
				fmt.Fprintf(w, "    Hint: %s\n", l.Hint)
			}
		}
		fmt.Fprintln(w)
	}

	return nil
}

type colorSet struct {
	bold, reset, red, yellow, green, cyan string
}

func colors(enabled bool) colorSet {
	if !enabled {
		return colorSet{}
	}
	return colorSet{
		bold:   "\033[1m",
		reset:  "\033[0m",
		red:    "\033[31m",
		yellow: "\033[33m",
		green:  "\033[32m",
		cyan:   "\033[36m",
	}
}

func statusColor(c colorSet, status string) string {
	switch status {
	case "critical":
		return c.red
	case "warning":
		return c.yellow
	case "ok":
		return c.green
	default:
		return c.cyan
	}
}

func severityColor(c colorSet, sev models.Severity) string {
	switch sev {
	case models.SeverityCritical:
		return c.red
	case models.SeverityWarning:
		return c.yellow
	default:
		return c.cyan
	}
}

func fallbackDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
