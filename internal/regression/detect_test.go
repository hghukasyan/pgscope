package regression

import (
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/internal/config"
	"github.com/hghukasyan/pgscope/pkg/models"
)

func series(name string, start time.Time, values []float64, step time.Duration) []models.MetricSample {
	out := make([]models.MetricSample, len(values))
	for i, v := range values {
		out[i] = models.MetricSample{
			Name: name, Value: v, Kind: models.MetricGauge,
			CollectedAt: start.Add(time.Duration(i) * step),
			Quality: models.QualityOK, InstanceKey: "i1", Database: "db",
		}
	}
	return out
}

func TestSustainedRegression(t *testing.T) {
	t.Parallel()
	cfg := config.RegressionConfig{
		Enabled: true, BaselineWindow: 50 * time.Minute, EvaluationWindow: 10 * time.Minute,
		MinSamples: 5, Sensitivity: "medium",
	}
	d := New(cfg)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	// 50 baseline samples ~1m apart at ~10, then 10 samples at ~40
	vals := make([]float64, 0, 60)
	for i := 0; i < 50; i++ {
		vals = append(vals, 10)
	}
	for i := 0; i < 10; i++ {
		vals = append(vals, 40)
	}
	s := series("query.mean_exec_time_ms", start, vals, time.Minute)
	now := start.Add(59 * time.Minute)
	r, ok := d.DetectSeries(s, now)
	if !ok {
		t.Fatal("expected regression")
	}
	if r.RelativeChange < 0.5 {
		t.Fatalf("rel=%v", r.RelativeChange)
	}
	if r.Reliability == "insufficient" {
		t.Fatal("reliability should not be insufficient")
	}
}

func TestTransientSpikeIgnored(t *testing.T) {
	t.Parallel()
	cfg := config.RegressionConfig{
		Enabled: true, BaselineWindow: 50 * time.Minute, EvaluationWindow: 10 * time.Minute,
		MinSamples: 5, Sensitivity: "medium",
	}
	d := New(cfg)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	vals := make([]float64, 0, 60)
	for i := 0; i < 55; i++ {
		vals = append(vals, 10)
	}
	vals = append(vals, 100) // single spike
	for i := 0; i < 4; i++ {
		vals = append(vals, 10)
	}
	s := series("query.mean_exec_time_ms", start, vals, time.Minute)
	now := start.Add(59 * time.Minute)
	_, ok := d.DetectSeries(s, now)
	if ok {
		t.Fatal("transient spike must not create incident-worthy regression")
	}
}

func TestInsufficientSamples(t *testing.T) {
	t.Parallel()
	d := New(config.RegressionConfig{Enabled: true, MinSamples: 10, BaselineWindow: time.Hour, EvaluationWindow: 10 * time.Minute})
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	s := series("locks.waiting_count", start, []float64{1, 2, 3}, time.Minute)
	_, ok := d.DetectSeries(s, start.Add(3*time.Minute))
	if ok {
		t.Fatal("expected no regression")
	}
}

func TestRecallRegressionLowerWorse(t *testing.T) {
	t.Parallel()
	cfg := config.RegressionConfig{
		Enabled: true, BaselineWindow: 50 * time.Minute, EvaluationWindow: 10 * time.Minute,
		MinSamples: 5, Sensitivity: "medium",
	}
	d := New(cfg)
	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	vals := make([]float64, 0, 60)
	for i := 0; i < 50; i++ {
		vals = append(vals, 0.95)
	}
	for i := 0; i < 10; i++ {
		vals = append(vals, 0.40)
	}
	s := series("vector.recall_at_k", start, vals, time.Minute)
	now := start.Add(59 * time.Minute)
	r, ok := d.DetectSeries(s, now)
	if !ok {
		t.Fatal("expected recall regression")
	}
	if r.RelativeChange >= 0 {
		t.Fatalf("expected negative relative change, got %v", r.RelativeChange)
	}
}
