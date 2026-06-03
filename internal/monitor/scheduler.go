// Package monitor runs continuous PostgreSQL observation with bounded concurrency.
package monitor

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hghukasyan/pgscope/internal/collector"
	"github.com/hghukasyan/pgscope/internal/config"
	"github.com/hghukasyan/pgscope/internal/diagnosis"
	"github.com/hghukasyan/pgscope/internal/history"
	"github.com/hghukasyan/pgscope/internal/incident"
	"github.com/hghukasyan/pgscope/internal/regression"
	"github.com/hghukasyan/pgscope/internal/telemetry"
	"github.com/hghukasyan/pgscope/pkg/models"
)

// Stats exposes internal monitor diagnostics.
type Stats struct {
	Cycles            int64 `json:"cycles"`
	SnapshotsSaved    int64 `json:"snapshots_saved"`
	CollectionErrors  int64 `json:"collection_errors"`
	PersistErrors     int64 `json:"persist_errors"`
	SkippedOverlaps   int64 `json:"skipped_overlaps"`
	LastDurationMS    int64 `json:"last_duration_ms"`
	LastError         string `json:"last_error,omitempty"`
	RegressionsFound  int64 `json:"regressions_found"`
	IncidentsTouched  int64 `json:"incidents_touched"`
}

// Service is the continuous monitoring agent.
type Service struct {
	cfg     config.Config
	col     *collector.Collector
	store   history.Store
	logger  *slog.Logger
	diag    *diagnosis.Engine
	regress *regression.Detector
	inc     *incident.Engine

	running atomic.Bool
	stats   Stats
	mu      sync.Mutex
}

// New creates a monitoring Service. store may be nil if history is disabled.
func New(cfg config.Config, col *collector.Collector, store history.Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	var incEng *incident.Engine
	if store != nil {
		incEng = incident.New(store, cfg.Incidents, cfg.Correlation)
	}
	return &Service{
		cfg:     cfg,
		col:     col,
		store:   store,
		logger:  logger,
		diag:    diagnosis.New(),
		regress: regression.New(cfg.Regression),
		inc:     incEng,
	}
}

// Stats returns a copy of internal counters.
func (s *Service) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

// Run blocks until ctx is cancelled, collecting on the configured interval.
func (s *Service) Run(ctx context.Context) error {
	interval := s.cfg.Monitoring.DefaultInterval
	if interval < time.Second {
		interval = 30 * time.Second
	}
	timeout := s.cfg.Monitoring.CollectionTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	s.logger.Info("monitor started", "interval", interval, "timeout", timeout)
	defer s.logger.Info("monitor stopped")

	// Initial immediate cycle.
	s.cycle(ctx, timeout)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			// Jitter up to 10% of interval to avoid synchronized bursts.
			jitter := time.Duration(rand.Int63n(int64(interval / 10)))
			timer := time.NewTimer(jitter)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			s.cycle(ctx, timeout)
		}
	}
}

func (s *Service) cycle(parent context.Context, timeout time.Duration) {
	if !s.running.CompareAndSwap(false, true) {
		s.mu.Lock()
		s.stats.SkippedOverlaps++
		s.mu.Unlock()
		s.logger.Warn("skipping collection: previous cycle still running")
		return
	}
	defer s.running.Store(false)

	start := time.Now()
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	s.mu.Lock()
	s.stats.Cycles++
	s.mu.Unlock()

	if s.col == nil {
		s.mu.Lock()
		s.stats.CollectionErrors++
		s.stats.LastError = "collector not configured"
		s.stats.LastDurationMS = time.Since(start).Milliseconds()
		s.mu.Unlock()
		return
	}
	snap, err := s.col.Collect(ctx)
	if err != nil {
		s.mu.Lock()
		s.stats.CollectionErrors++
		s.stats.LastError = err.Error()
		s.stats.LastDurationMS = time.Since(start).Milliseconds()
		s.mu.Unlock()
		s.logger.Error("collection failed", "err", err)
		return
	}

	result, err := s.diag.Analyze(ctx, snap)
	if err != nil {
		s.logger.Warn("diagnosis failed", "err", err)
		result.Findings = nil
	}

	metrics := telemetry.ExtractMetrics(snap)
	if s.store != nil && s.cfg.History.Enabled {
		meta := models.HistorySnapshotMeta{
			ID: snap.ID, CollectedAt: snap.CollectedAt,
			InstanceKey: telemetry.InstanceKey(snap.Instance),
			Database: snap.Instance.Database,
			PGVersion: snap.Instance.VersionString,
			DurationMS: snap.DurationMS,
		}
		if err := s.store.SaveSnapshot(ctx, meta, result.Findings, metrics); err != nil {
			s.mu.Lock()
			s.stats.PersistErrors++
			s.stats.LastError = err.Error()
			s.mu.Unlock()
			s.logger.Error("persist snapshot failed", "err", err)
			// Continue evaluation using in-memory metrics only for this cycle's detection
			// would be incomplete; skip regression if we can't read history.
		} else {
			s.mu.Lock()
			s.stats.SnapshotsSaved++
			s.mu.Unlock()
		}

		// Retention cleanup occasionally.
		if s.stats.Cycles%20 == 0 {
			_, _ = s.store.Cleanup(ctx, history.RetentionPolicy{
				RawMetrics: s.cfg.History.Retention.RawMetrics,
				Incidents:  s.cfg.History.Retention.Incidents,
			})
		}

		if s.cfg.Regression.Enabled {
			lookback := s.cfg.Regression.BaselineWindow + s.cfg.Regression.EvaluationWindow + time.Minute
			since := time.Now().UTC().Add(-lookback)
			hist, err := s.store.ListMetrics(ctx, "", since, time.Time{}, 10000)
			if err != nil {
				s.logger.Warn("list metrics failed", "err", err)
			} else {
				// Merge just-saved samples (already in DB) — hist is enough.
				regs := s.regress.DetectGroups(hist, time.Now().UTC())
				if len(regs) > 0 {
					if err := s.store.SaveRegressions(ctx, regs); err != nil {
						s.logger.Warn("save regressions failed", "err", err)
					}
					s.mu.Lock()
					s.stats.RegressionsFound += int64(len(regs))
					s.mu.Unlock()
					if s.inc != nil {
						incs, err := s.inc.ProcessRegressions(ctx, regs, result.Findings, time.Now().UTC())
						if err != nil {
							s.logger.Warn("incident processing failed", "err", err)
						} else {
							s.mu.Lock()
							s.stats.IncidentsTouched += int64(len(incs))
							s.mu.Unlock()
						}
					}
				}
			}
		}
	}

	s.mu.Lock()
	s.stats.LastDurationMS = time.Since(start).Milliseconds()
	s.stats.LastError = ""
	s.mu.Unlock()
	s.logger.Info("monitor cycle complete",
		"snapshot", snap.ID,
		"metrics", len(metrics),
		"findings", len(result.Findings),
		"duration_ms", time.Since(start).Milliseconds())
}

// RunOnce executes a single collection/persist/detect cycle (for CLI one-shots).
func (s *Service) RunOnce(ctx context.Context) error {
	timeout := s.cfg.Monitoring.CollectionTimeout
	if timeout <= 0 {
		timeout = s.cfg.Timeout
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	s.cycle(ctx, timeout)
	st := s.Stats()
	if st.LastError != "" && st.SnapshotsSaved == 0 && st.CollectionErrors > 0 {
		return fmt.Errorf("%s", st.LastError)
	}
	return nil
}
