package history

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// PersistRecallReport stores compact vector benchmark summaries and metric samples.
// Does not store embeddings or full sample ID lists.
func PersistRecallReport(ctx context.Context, store Store, instanceKey string, rep models.RecallReport) error {
	if store == nil {
		return fmt.Errorf("history store is nil")
	}
	at := rep.CollectedAt
	if at.IsZero() {
		at = time.Now().UTC()
	}
	var metrics []models.MetricSample
	for _, sum := range rep.Summaries {
		id := newHistID("vb")
		summary := VectorBenchmarkSummary{
			ID: id, CollectedAt: at, InstanceKey: instanceKey, Database: rep.Database,
			WorkloadFingerprint: rep.WorkloadFingerprint, ConfigFingerprint: rep.ConfigFingerprint,
			RecallAtK: sum.MeanRecall, K: sum.K,
			P50LatencyMS: sum.ApproxLatency.P50MS, P95LatencyMS: sum.ApproxLatency.P95MS,
			SampleCount: sum.Samples, IndexName: rep.IndexName,
			Notes: "from pgscope vector recall; embeddings not stored",
		}
		if err := store.SaveVectorBenchmark(ctx, summary); err != nil {
			return err
		}
		dims := map[string]string{
			"k": strconv.Itoa(sum.K),
			"workload": rep.WorkloadFingerprint,
			"config": rep.ConfigFingerprint,
		}
		snapID := "vec-" + id
		metrics = append(metrics,
			models.MetricSample{
				SnapshotID: snapID, CollectedAt: at, InstanceKey: instanceKey, Database: rep.Database,
				Name: "vector.recall_at_k", Value: sum.MeanRecall, Kind: models.MetricBenchmark,
				Unit: "ratio", Dimensions: dims, Source: "vector_recall", Quality: models.QualityOK,
			},
			models.MetricSample{
				SnapshotID: snapID, CollectedAt: at, InstanceKey: instanceKey, Database: rep.Database,
				Name: "vector.approx_p95_latency_ms", Value: sum.ApproxLatency.P95MS, Kind: models.MetricBenchmark,
				Unit: "ms", Dimensions: dims, Source: "vector_recall", Quality: models.QualityOK,
			},
		)
	}
	if len(metrics) == 0 {
		return nil
	}
	meta := models.HistorySnapshotMeta{
		ID: "vecbench-" + newHistID(""), CollectedAt: at, InstanceKey: instanceKey,
		Database: rep.Database, PGVersion: rep.PGVersion, DurationMS: rep.DurationMS, Status: "vector_benchmark",
	}
	return store.SaveSnapshot(ctx, meta, nil, metrics)
}

func newHistID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	if prefix == "" {
		return hex.EncodeToString(b[:])
	}
	return prefix + "-" + hex.EncodeToString(b[:])
}
