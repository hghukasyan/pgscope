// Package database provides safe, read-oriented PostgreSQL connectivity.
package database

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Pool wraps a bounded pgx connection pool with safety defaults applied.
type Pool struct {
	*pgxpool.Pool
	cfg config.Config
}

// Connect opens a bounded pool and applies session safety GUCs.
func Connect(ctx context.Context, cfg config.Config) (*Pool, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("empty DSN")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}

	poolCfg.MaxConns = cfg.MaxConns
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	poolCfg.MaxConnLifetime = 30 * time.Minute
	poolCfg.HealthCheckPeriod = 30 * time.Second
	poolCfg.ConnConfig.ConnectTimeout = cfg.ConnectTimeout
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	poolCfg.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {
		return applySafetyGUCs(ctx, conn, cfg)
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.ConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}

	return &Pool{Pool: pool, cfg: cfg}, nil
}

func applySafetyGUCs(ctx context.Context, conn *pgx.Conn, cfg config.Config) error {
	stmts := []string{
		fmt.Sprintf("SET statement_timeout = %d", cfg.StatementTimeout.Milliseconds()),
		fmt.Sprintf("SET lock_timeout = %d", cfg.LockTimeout.Milliseconds()),
		"SET idle_in_transaction_session_timeout = 60000",
		"SET application_name = 'pgscope'",
	}
	if cfg.ReadOnly {
		stmts = append(stmts, "SET default_transaction_read_only = on")
	}
	for _, s := range stmts {
		if _, err := conn.Exec(ctx, s); err != nil {
			return fmt.Errorf("apply safety guc %q: %w", s, err)
		}
	}
	return nil
}

// RedactDSN removes password material from a connection string for display/logging.
func RedactDSN(dsn string) string {
	if dsn == "" {
		return ""
	}
	// URL form
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" && u.User != nil {
		username := u.User.Username()
		_, hasPassword := u.User.Password()
		u.User = url.User(username)
		out := u.String()
		if hasPassword {
			// Avoid url.UserPassword — it percent-encodes '*' as %2A.
			needle := "://" + username + "@"
			repl := "://" + username + ":****@"
			if username == "" {
				needle = "://@"
				repl = "://:****@"
			}
			out = strings.Replace(out, needle, repl, 1)
		}
		return out
	}
	// keyword/value form
	parts := strings.Fields(dsn)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		lower := strings.ToLower(p)
		if strings.HasPrefix(lower, "password=") {
			out = append(out, "password=****")
			continue
		}
		out = append(out, p)
	}
	return strings.Join(out, " ")
}

// Config returns the config used to create the pool.
func (p *Pool) Config() config.Config {
	return p.cfg
}
