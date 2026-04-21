package telemetry

import (
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func sample(name string, v float64, at time.Time, reset string) models.MetricSample {
	return models.MetricSample{
		Name: name, Value: v, Kind: models.MetricCounter,
		CollectedAt: at, ResetToken: reset, Quality: models.QualityOK,
	}
}

func TestCounterDeltaOK(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	prev := sample("wal.bytes", 1000, t0, "a")
	curr := sample("wal.bytes", 2500, t0.Add(10*time.Second), "a")
	d := CounterDelta(prev, curr)
	if d.Quality != models.QualityOK {
		t.Fatalf("quality=%s reason=%s", d.Quality, d.Reason)
	}
	if d.Delta != 1500 {
		t.Fatalf("delta=%v", d.Delta)
	}
	if d.RatePerSec != 150 {
		t.Fatalf("rate=%v", d.RatePerSec)
	}
}

func TestCounterDeltaResetOnDecrease(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	prev := sample("wal.bytes", 5000, t0, "a")
	curr := sample("wal.bytes", 100, t0.Add(10*time.Second), "a")
	d := CounterDelta(prev, curr)
	if d.Quality != models.QualityReset {
		t.Fatalf("expected reset, got %s", d.Quality)
	}
	if d.RatePerSec != 0 {
		t.Fatalf("must not invent rate from negative delta")
	}
}

func TestCounterDeltaResetTokenChange(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	prev := sample("wal.bytes", 1000, t0, "boot1")
	curr := sample("wal.bytes", 2000, t0.Add(10*time.Second), "boot2")
	d := CounterDelta(prev, curr)
	if d.Quality != models.QualityReset {
		t.Fatalf("expected reset token change, got %s", d.Quality)
	}
}

func TestCounterDeltaBadTimestamps(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	prev := sample("wal.bytes", 1000, t0, "a")
	curr := sample("wal.bytes", 2000, t0, "a")
	d := CounterDelta(prev, curr)
	if d.Quality != models.QualityAnomalous {
		t.Fatalf("got %s", d.Quality)
	}
}

func TestDeriveRatesInsufficientFirst(t *testing.T) {
	t.Parallel()
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	series := []models.MetricSample{
		sample("calls", 10, t0, "a"),
		sample("calls", 30, t0.Add(10*time.Second), "a"),
	}
	out := DeriveRates(series)
	if len(out) < 4 {
		t.Fatalf("len=%d", len(out))
	}
	// second rate should be OK: (30-10)/10 = 2
	var found bool
	for _, m := range out {
		if m.Name == "calls.rate" && m.Quality == models.QualityOK {
			found = true
			if m.Value != 2 {
				t.Fatalf("rate=%v", m.Value)
			}
		}
	}
	if !found {
		t.Fatal("expected ok rate sample")
	}
}
