// Package regression detects performance regressions from historical metric series.
package regression

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/hghukasyan/pgscope/internal/config"
	"github.com/hghukasyan/pgscope/pkg/models"
)

// Detector evaluates metric series against baselines.
type Detector struct {
	cfg config.RegressionConfig
}

// New creates a Detector.
func New(cfg config.RegressionConfig) *Detector {
	if cfg.MinSamples <= 0 {
		cfg.MinSamples = 5
	}
	if cfg.BaselineWindow <= 0 {
		cfg.BaselineWindow = time.Hour
	}
	if cfg.EvaluationWindow <= 0 {
		cfg.EvaluationWindow = 10 * time.Minute
	}
	if cfg.Sensitivity == "" {
		cfg.Sensitivity = "medium"
	}
	return &Detector{cfg: cfg}
}

// thresholds returns relative change and MAD multiplier for sensitivity.
func (d *Detector) thresholds() (rel float64, madMult float64, sustained int) {
	switch d.cfg.Sensitivity {
	case "low":
		return 0.75, 5, 3
	case "high":
		return 0.25, 3, 2
	default: // medium
		return 0.50, 4, 2
	}
}

// DetectSeries compares evaluation window samples against a rolling baseline.
// series must be ascending by CollectedAt and comparable (same name/dims).
func (d *Detector) DetectSeries(series []models.MetricSample, now time.Time) (models.Regression, bool) {
	if !d.cfg.Enabled || len(series) == 0 {
		return models.Regression{}, false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	evalStart := now.Add(-d.cfg.EvaluationWindow)
	baseStart := evalStart.Add(-d.cfg.BaselineWindow)
	baseEnd := evalStart

	var baseline, current []float64
	var baseTimes, curTimes []time.Time
	var meta models.MetricSample
	for _, s := range series {
		if s.Quality != models.QualityOK && s.Quality != "" {
			continue
		}
		if s.CollectedAt.Before(baseStart) || s.CollectedAt.After(now) {
			continue
		}
		meta = s
		if !s.CollectedAt.Before(evalStart) {
			current = append(current, s.Value)
			curTimes = append(curTimes, s.CollectedAt)
		} else if !s.CollectedAt.After(baseEnd) {
			baseline = append(baseline, s.Value)
			baseTimes = append(baseTimes, s.CollectedAt)
		}
	}

	minSamples := d.cfg.MinSamples
	if len(baseline) < minSamples || len(current) < minSamples {
		return models.Regression{
			MetricName:  meta.Name,
			Reliability: "insufficient",
			SampleCount: len(current),
			BaselineSamples: len(baseline),
			Method: "rolling_median_mad",
		}, false
	}

	baseMed := median(baseline)
	curMed := median(current)
	if baseMed == 0 && curMed == 0 {
		return models.Regression{}, false
	}

	relThresh, madMult, sustained := d.thresholds()
	absChange := curMed - baseMed
	var relChange float64
	if baseMed != 0 {
		relChange = absChange / math.Abs(baseMed)
	} else if curMed != 0 {
		relChange = math.Inf(1)
	}

	mad := medianAbsDev(baseline, baseMed)
	// Robust threshold: max(relative, MAD-based).
	madGate := madMult * mad
	if madGate == 0 {
		madGate = math.Abs(baseMed) * relThresh
	}

	// Require sustained breach: enough current samples above baseline+gate.
	breachCount := 0
	gate := baseMed + madGate
	// For metrics where lower is worse (recall), invert.
	lowerIsWorse := isLowerWorse(meta.Name)
	if lowerIsWorse {
		gate = baseMed - madGate
		for _, v := range current {
			if v < gate || (baseMed != 0 && (baseMed-v)/math.Abs(baseMed) >= relThresh) {
				breachCount++
			}
		}
	} else {
		for _, v := range current {
			if v > gate || (baseMed != 0 && (v-baseMed)/math.Abs(baseMed) >= relThresh) {
				breachCount++
			}
		}
	}

	regressed := false
	if lowerIsWorse {
		regressed = relChange <= -relThresh && breachCount >= sustained
	} else {
		regressed = relChange >= relThresh && breachCount >= sustained
	}
	// Also accept MAD absolute breach when relative baseline is tiny.
	if !regressed && mad > 0 {
		if lowerIsWorse && curMed < baseMed-madGate && breachCount >= sustained {
			regressed = true
		}
		if !lowerIsWorse && curMed > baseMed+madGate && breachCount >= sustained {
			regressed = true
		}
	}
	if !regressed {
		return models.Regression{}, false
	}

	severity := models.SeverityWarning
	mag := math.Abs(relChange)
	if mag >= 1.0 || (lowerIsWorse && mag >= 0.2) {
		severity = models.SeverityCritical
	}
	reliability := "moderate"
	if len(baseline) >= minSamples*2 && len(current) >= minSamples && mad > 0 {
		reliability = "strong"
	}
	if len(baseline) < minSamples+2 {
		reliability = "weak"
	}

	windowStart, windowEnd := curTimes[0], curTimes[len(curTimes)-1]
	bStart, bEnd := baseTimes[0], baseTimes[len(baseTimes)-1]

	return models.Regression{
		ID:              newID("reg"),
		DetectedAt:      now,
		MetricName:      meta.Name,
		Dimensions:      meta.Dimensions,
		InstanceKey:     meta.InstanceKey,
		Database:        meta.Database,
		CurrentValue:    curMed,
		BaselineValue:   baseMed,
		AbsoluteChange:  absChange,
		RelativeChange:  relChange,
		WindowStart:     windowStart,
		WindowEnd:       windowEnd,
		BaselineStart:   bStart,
		BaselineEnd:     bEnd,
		SampleCount:     len(current),
		BaselineSamples: len(baseline),
		Method:          "rolling_median_mad",
		Threshold:       fmt.Sprintf("rel>=%.0f%% mad_mult=%.0f sustained=%d", relThresh*100, madMult, sustained),
		Severity:        severity,
		Reliability:     reliability,
		Evidence: []models.Evidence{
			{Kind: models.EvidenceFact, Source: "history", Description: "baseline median", Value: fmt.Sprintf("%.4g", baseMed)},
			{Kind: models.EvidenceFact, Source: "history", Description: "evaluation median", Value: fmt.Sprintf("%.4g", curMed)},
			{Kind: models.EvidenceFact, Source: "history", Description: "MAD", Value: fmt.Sprintf("%.4g", mad)},
			{Kind: models.EvidenceFact, Source: "history", Description: "sustained breaches", Value: fmt.Sprintf("%d/%d", breachCount, len(current))},
		},
	}, true
}

// DetectGroups runs detection across metric groups keyed by name+dimensions.
func (d *Detector) DetectGroups(samples []models.MetricSample, now time.Time) []models.Regression {
	groups := map[string][]models.MetricSample{}
	for _, s := range samples {
		key := s.Name + "|" + dimKey(s.Dimensions)
		groups[key] = append(groups[key], s)
	}
	var out []models.Regression
	for _, series := range groups {
		sort.Slice(series, func(i, j int) bool {
			return series[i].CollectedAt.Before(series[j].CollectedAt)
		})
		if r, ok := d.DetectSeries(series, now); ok {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return math.Abs(out[i].RelativeChange) > math.Abs(out[j].RelativeChange)
	})
	return out
}

func isLowerWorse(name string) bool {
	switch name {
	case "vector.recall", "vector.recall_at_k", "query.cache_hit_ratio":
		return true
	default:
		return false
	}
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	cp := append([]float64(nil), vals...)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

func medianAbsDev(vals []float64, med float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	devs := make([]float64, len(vals))
	for i, v := range vals {
		devs[i] = math.Abs(v - med)
	}
	return median(devs)
}

func dimKey(dims map[string]string) string {
	if len(dims) == 0 {
		return ""
	}
	keys := make([]string, 0, len(dims))
	for k := range dims {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += ","
		}
		out += k + "=" + dims[k]
	}
	return out
}

func newID(prefix string) string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return prefix + "-" + hex.EncodeToString(b[:])
}
