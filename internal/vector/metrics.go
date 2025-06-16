package vector

import (
	"fmt"
	"math"
	"sort"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// RecallAtK computes |approx ∩ exact| / |exact| with deterministic tie handling via ID sets.
// If exact is empty, recall is defined as 1.0 when approx is also empty, else 0.0.
func RecallAtK(exactIDs, approxIDs []string) (intersection int, recall float64) {
	if len(exactIDs) == 0 {
		if len(approxIDs) == 0 {
			return 0, 1
		}
		return 0, 0
	}
	set := map[string]struct{}{}
	for _, id := range exactIDs {
		set[id] = struct{}{}
	}
	for _, id := range approxIDs {
		if _, ok := set[id]; ok {
			intersection++
			delete(set, id) // count unique matches once
		}
	}
	recall = float64(intersection) / float64(len(exactIDs))
	return intersection, recall
}

// LatencyFromSamples builds percentile stats. Warns when p99 is under-sampled.
func LatencyFromSamples(samplesMS []float64) models.LatencyStats {
	st := models.LatencyStats{Count: len(samplesMS)}
	if len(samplesMS) == 0 {
		return st
	}
	sorted := append([]float64(nil), samplesMS...)
	sort.Float64s(sorted)
	var sum float64
	for _, v := range sorted {
		sum += v
	}
	st.MeanMS = sum / float64(len(sorted))
	st.MinMS = sorted[0]
	st.MaxMS = sorted[len(sorted)-1]
	st.P50MS = percentile(sorted, 50)
	st.P90MS = percentile(sorted, 90)
	st.P95MS = percentile(sorted, 95)
	st.P99MS = percentile(sorted, 99)
	if len(sorted) < 100 {
		st.P99Warning = fmt.Sprintf("p99 estimated from only %d samples; treat as unstable", len(sorted))
	}
	return st
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	lo := int(math.Floor(rank))
	hi := int(math.Ceil(rank))
	if lo == hi {
		return sorted[lo]
	}
	w := rank - float64(lo)
	return sorted[lo]*(1-w) + sorted[hi]*w
}

// SummarizeRecall aggregates samples for a given K.
func SummarizeRecall(samples []models.RecallSample, k int) models.RecallSummary {
	sum := models.RecallSummary{
		K:               k,
		DenominatorNote: "Recall@K = |ApproxTopK ∩ ExactTopK| / |ExactTopK|; when fewer than K eligible rows exist, |ExactTopK| is the available exact result count.",
	}
	var recalls []float64
	var approxLat, exactLat []float64
	minR := 1.0
	for _, s := range samples {
		if s.K != k {
			continue
		}
		if s.Error != "" {
			sum.Errors++
			continue
		}
		sum.Samples++
		recalls = append(recalls, s.Recall)
		if s.Recall < minR {
			minR = s.Recall
		}
		approxLat = append(approxLat, s.ApproxLatencyMS)
		exactLat = append(exactLat, s.ExactLatencyMS)
		if s.IncompleteResult {
			sum.IncompleteResults++
		}
	}
	if len(recalls) == 0 {
		sum.MinRecall = 0
		return sum
	}
	var total float64
	for _, r := range recalls {
		total += r
	}
	sum.MeanRecall = total / float64(len(recalls))
	sum.MinRecall = minR
	sum.ApproxLatency = LatencyFromSamples(approxLat)
	sum.ExactLatency = LatencyFromSamples(exactLat)
	return sum
}
