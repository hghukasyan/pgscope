package telemetry

import (
	"math"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// DeltaResult is a counter rate between two samples.
type DeltaResult struct {
	Delta     float64
	RatePerSec float64
	Interval  time.Duration
	Quality   models.MetricQuality
	Reason    string
}

// CounterDelta computes a safe delta/rate between previous and current counter samples.
// Negative deltas, resets, and missing/invalid intervals yield QualityReset/Unavailable
// and never invent positive rates from garbage.
func CounterDelta(prev, curr models.MetricSample) DeltaResult {
	if prev.Name != curr.Name {
		return DeltaResult{Quality: models.QualityUnavailable, Reason: "metric name mismatch"}
	}
	if DimKey(prev.Dimensions) != DimKey(curr.Dimensions) {
		return DeltaResult{Quality: models.QualityUnavailable, Reason: "dimension mismatch"}
	}
	if prev.CollectedAt.IsZero() || curr.CollectedAt.IsZero() {
		return DeltaResult{Quality: models.QualityUnavailable, Reason: "missing timestamp"}
	}
	if !curr.CollectedAt.After(prev.CollectedAt) {
		return DeltaResult{Quality: models.QualityAnomalous, Reason: "non-increasing timestamps"}
	}
	interval := curr.CollectedAt.Sub(prev.CollectedAt)
	if interval < time.Second {
		return DeltaResult{Quality: models.QualityAnomalous, Reason: "interval too short", Interval: interval}
	}
	// Cap absurd gaps (e.g. clock jumps / long downtime) as unavailable rates.
	if interval > 24*time.Hour {
		return DeltaResult{Quality: models.QualityAnomalous, Reason: "interval too long", Interval: interval}
	}

	if prev.ResetToken != "" && curr.ResetToken != "" && prev.ResetToken != curr.ResetToken {
		return DeltaResult{Quality: models.QualityReset, Reason: "reset token changed", Interval: interval}
	}

	delta := curr.Value - prev.Value
	if delta < 0 {
		return DeltaResult{
			Delta:    delta,
			Interval: interval,
			Quality:  models.QualityReset,
			Reason:   "counter decreased (likely stats reset or restart)",
		}
	}
	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return DeltaResult{Quality: models.QualityAnomalous, Reason: "non-finite value", Interval: interval}
	}

	secs := interval.Seconds()
	return DeltaResult{
		Delta:      delta,
		RatePerSec: delta / secs,
		Interval:   interval,
		Quality:    models.QualityOK,
	}
}

// DeriveRates walks an ascending series of samples and emits derived rate samples
// for counter metrics. Gauge/derived/benchmark samples are passed through.
func DeriveRates(series []models.MetricSample) []models.MetricSample {
	if len(series) == 0 {
		return nil
	}
	out := make([]models.MetricSample, 0, len(series))
	var prev *models.MetricSample
	for i := range series {
		cur := series[i]
		if cur.Kind != models.MetricCounter {
			out = append(out, cur)
			prev = &series[i]
			continue
		}
		if prev == nil || prev.Kind != models.MetricCounter || prev.Name != cur.Name || DimKey(prev.Dimensions) != DimKey(cur.Dimensions) {
			// First sample: keep raw counter, mark rate unavailable.
			rate := cur
			rate.Kind = models.MetricDerived
			rate.Name = cur.Name + ".rate"
			rate.Value = 0
			rate.Quality = models.QualityInsufficient
			rate.Unit = cur.Unit + "/s"
			out = append(out, cur, rate)
			prev = &series[i]
			continue
		}
		d := CounterDelta(*prev, cur)
		rate := cur
		rate.Kind = models.MetricDerived
		rate.Name = cur.Name + ".rate"
		rate.Unit = cur.Unit + "/s"
		rate.Quality = d.Quality
		if d.Quality == models.QualityOK {
			rate.Value = d.RatePerSec
		} else {
			rate.Value = 0
		}
		out = append(out, cur, rate)
		prev = &series[i]
	}
	return out
}
