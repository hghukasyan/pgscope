// Package collector gathers PostgreSQL observations into a Snapshot.
package collector

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/hghukasyan/pgscope/internal/config"
	"github.com/hghukasyan/pgscope/internal/database"
	"github.com/hghukasyan/pgscope/pkg/models"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Collector builds diagnostic snapshots from a live PostgreSQL instance.
type Collector struct {
	pool   *pgxpool.Pool
	cfg    config.Config
	logger *slog.Logger
}

// New creates a Collector.
func New(pool *database.Pool, cfg config.Config, logger *slog.Logger) *Collector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Collector{pool: pool.Pool, cfg: cfg, logger: logger}
}

// Collect runs collectors and returns a Snapshot. Phase-2 collectors are
// included when enabled in config (defaults on, lightweight limits).
func (c *Collector) Collect(ctx context.Context) (models.Snapshot, error) {
	start := time.Now()
	snap := models.Snapshot{
		ID:          newID(),
		CollectedAt: start.UTC(),
		DeepScan:    c.cfg.DeepScan,
	}

	inst, lims, err := c.collectInstance(ctx)
	snap.Limitations = append(snap.Limitations, lims...)
	if err != nil {
		return snap, fmt.Errorf("collect instance: %w", err)
	}
	snap.Instance = inst

	conns, lims := c.collectConnections(ctx, inst)
	snap.Limitations = append(snap.Limitations, lims...)
	snap.Connections = conns
	snap.Instance.Capabilities.CanReadActivity = conns.Collected

	txns, lims := c.collectTransactions(ctx, inst)
	snap.Limitations = append(snap.Limitations, lims...)
	snap.Transactions = txns

	locks, lims := c.collectLocks(ctx)
	snap.Limitations = append(snap.Limitations, lims...)
	snap.Locks = locks
	snap.Instance.Capabilities.CanReadLocks = locks.Collected

	storage, lims := c.collectStorage(ctx, inst.Database)
	snap.Limitations = append(snap.Limitations, lims...)
	snap.Storage = storage
	snap.Instance.Capabilities.CanReadDatabaseSize = storage.Collected

	indexes, lims := c.collectIndexes(ctx)
	snap.Limitations = append(snap.Limitations, lims...)
	snap.Indexes = indexes
	snap.Instance.Capabilities.CanReadRelationStats = indexes.Collected

	queries, lims := c.collectQueries(ctx, models.QuerySortKey(c.cfg.QuerySort), c.cfg.TopQueries)
	snap.Limitations = append(snap.Limitations, lims...)
	snap.Queries = queries

	vacuum, lims := c.collectVacuum(ctx, "")
	snap.Limitations = append(snap.Limitations, lims...)
	snap.Vacuum = vacuum
	snap.Instance.Capabilities.CanReadVacuumProgress = len(vacuum.Progress) > 0 || vacuum.Collected

	wal, lims := c.collectWAL(ctx)
	snap.Limitations = append(snap.Limitations, lims...)
	snap.WAL = wal
	snap.Instance.Capabilities.CanReadWAL = wal.Collected

	repl, lims := c.collectReplication(ctx)
	snap.Limitations = append(snap.Limitations, lims...)
	snap.Replication = repl
	snap.Instance.Capabilities.CanReadReplication = repl.Collected

	vec, lims := c.collectVector(ctx)
	snap.Limitations = append(snap.Limitations, lims...)
	snap.Vector = vec
	if vec.Available {
		snap.Instance.Capabilities.HasPgVector = true
	}

	snap.DurationMS = time.Since(start).Milliseconds()
	return snap, nil
}

func newID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	needles := []string{
		"permission denied",
		"must be superuser",
		"must be owner",
		"insufficient privilege",
		"not permitted",
	}
	for _, n := range needles {
		if strings.Contains(msg, n) {
			return true
		}
	}
	return false
}
