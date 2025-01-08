package collector

import "testing"

func TestComputeVacuumThreshold(t *testing.T) {
	t.Parallel()
	got := ComputeVacuumThreshold(50, 0.2, 10000, nil)
	if got != 2050 {
		t.Fatalf("got %d want 2050", got)
	}
	max := int64(1000)
	got = ComputeVacuumThreshold(50, 0.2, 10000, &max)
	if got != 1000 {
		t.Fatalf("max threshold not applied: %d", got)
	}
}

func TestComputeVacuumThresholdPerTableOverrideMath(t *testing.T) {
	t.Parallel()
	// Reloption-style tighter scale factor.
	got := ComputeVacuumThreshold(50, 0.01, 1_000_000, nil)
	if got != 10050 {
		t.Fatalf("got %d", got)
	}
}
