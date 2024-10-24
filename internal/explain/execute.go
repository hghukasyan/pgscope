package explain

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/internal/sqlsafe"
	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunOptions configures a safe EXPLAIN execution.
type RunOptions struct {
	Analyze         bool
	Buffers         bool
	Settings        bool
	Verbose         bool
	Wal             bool
	TrustedIsolated bool
	Timeout         time.Duration
	PreviewLen      int
	Database        string
}

// Run validates SQL, executes EXPLAIN (FORMAT JSON), parses and analyzes the plan.
func Run(ctx context.Context, pool *pgxpool.Pool, sql string, opts RunOptions) (models.ExplainResult, error) {
	var out models.ExplainResult
	var val sqlsafe.Result
	var err error
	if opts.Analyze {
		val, err = sqlsafe.ValidateExplainAnalyze(sql, opts.TrustedIsolated)
	} else {
		val, err = sqlsafe.ValidateReadOnlySelect(sql)
	}
	if err != nil {
		return out, err
	}
	out.SQLPreview = database.SanitizeQueryPreview(val.SQL, opts.PreviewLen)
	out.Analyze = opts.Analyze

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return out, fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	tx, err := conn.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, fmt.Errorf("begin read-only transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	explainSQL := buildExplain(val.SQL, opts)
	var jsonText string
	if err := tx.QueryRow(ctx, explainSQL).Scan(&jsonText); err != nil {
		return out, fmt.Errorf("EXPLAIN failed: %w", err)
	}
	parsed, err := ParseJSON([]byte(jsonText))
	if err != nil {
		return out, err
	}
	parsed.SQLPreview = out.SQLPreview
	parsed.Analyze = opts.Analyze
	AnalyzePlan(&parsed, opts.Database)
	return parsed, nil
}

func buildExplain(sql string, opts RunOptions) string {
	var optsParts []string
	optsParts = append(optsParts, "FORMAT JSON")
	if opts.Analyze {
		optsParts = append(optsParts, "ANALYZE true")
		if opts.Buffers {
			optsParts = append(optsParts, "BUFFERS true")
		}
		if opts.Wal {
			optsParts = append(optsParts, "WAL true")
		}
	} else {
		optsParts = append(optsParts, "ANALYZE false")
	}
	if opts.Verbose {
		optsParts = append(optsParts, "VERBOSE true")
	}
	if opts.Settings {
		optsParts = append(optsParts, "SETTINGS true")
	}
	// EXPLAIN does not support parameters for the statement body; SQL was validated.
	return fmt.Sprintf("EXPLAIN (%s) %s", strings.Join(optsParts, ", "), sql)
}
