package collector

import (
	"testing"
	"time"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestDeriveWALRates(t *testing.T) {
	t.Parallel()
	prevBytes := int64(1000)
	curBytes := int64(5000)
	prev := models.WALStats{WALBytes: &prevBytes}
	cur := models.WALStats{WALBytes: &curBytes}
	out := DeriveWALRates(prev, cur, 4*time.Second)
	if !out.RateValid || out.WALBytesPerSec == nil || *out.WALBytesPerSec != 1000 {
		t.Fatalf("unexpected rate: %+v", out)
	}
}

func TestDeriveWALRatesCounterReset(t *testing.T) {
	t.Parallel()
	prevBytes := int64(5000)
	curBytes := int64(100)
	prev := models.WALStats{WALBytes: &prevBytes}
	cur := models.WALStats{WALBytes: &curBytes}
	out := DeriveWALRates(prev, cur, time.Second)
	if out.RateValid {
		t.Fatal("expected invalid rate on counter decrease")
	}
}

func TestDeriveWALRatesStatsReset(t *testing.T) {
	t.Parallel()
	t1 := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	b1, b2 := int64(1), int64(100)
	prev := models.WALStats{WALBytes: &b1, StatsReset: &t1}
	cur := models.WALStats{WALBytes: &b2, StatsReset: &t2}
	out := DeriveWALRates(prev, cur, time.Second)
	if out.RateValid {
		t.Fatal("expected invalid after stats reset")
	}
}

func TestLSNByteDiff(t *testing.T) {
	t.Parallel()
	if LSNByteDiff(10, 40) != 30 {
		t.Fatal("diff")
	}
}
