package vector

import (
	"testing"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestRecallAtK(t *testing.T) {
	t.Parallel()
	inter, rec := RecallAtK([]string{"1", "2", "3"}, []string{"3", "4", "1"})
	if inter != 2 || rec < 0.66 || rec > 0.67 {
		t.Fatalf("inter=%d rec=%f", inter, rec)
	}
	_, rec = RecallAtK(nil, nil)
	if rec != 1 {
		t.Fatalf("empty/empty want 1 got %f", rec)
	}
	_, rec = RecallAtK(nil, []string{"1"})
	if rec != 0 {
		t.Fatalf("empty exact want 0 got %f", rec)
	}
}

func TestLatencyPercentiles(t *testing.T) {
	t.Parallel()
	st := LatencyFromSamples([]float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	if st.P50MS < 5 || st.P50MS > 6 {
		t.Fatalf("p50=%f", st.P50MS)
	}
	if st.P99Warning == "" {
		t.Fatal("expected p99 warning for small sample")
	}
}

func TestSummarizeRecall(t *testing.T) {
	t.Parallel()
	samples := []models.RecallSample{
		{K: 10, Recall: 1.0},
		{K: 10, Recall: 0.8},
		{K: 5, Recall: 0.5},
	}
	sum := SummarizeRecall(samples, 10)
	if sum.Samples != 2 || sum.MeanRecall < 0.89 || sum.MeanRecall > 0.91 {
		t.Fatalf("%+v", sum)
	}
}

func TestWorkloadValidationRejectsProduction(t *testing.T) {
	t.Parallel()
	w := Workload{
		Target: WorkloadTarget{Schema: "public", Table: "documents", IDColumn: "id", VectorColumn: "embedding"},
		Benchmark: WorkloadBenchmark{Environment: "production", Samples: 10},
	}
	if err := w.Validate(); err == nil {
		t.Fatal("expected production env rejection")
	}
}

func TestWorkloadRejectsBadFilterOp(t *testing.T) {
	t.Parallel()
	w := Workload{
		Target: WorkloadTarget{Schema: "public", Table: "documents", IDColumn: "id", VectorColumn: "embedding"},
		Search: WorkloadSearch{Filter: &WorkloadFilter{Column: "tenant_id", Operator: "LIKE", Values: []string{"x"}}},
	}
	if err := w.Validate(); err == nil {
		t.Fatal("expected bad operator rejection")
	}
}

func TestQuoteIdent(t *testing.T) {
	t.Parallel()
	q, err := QuoteIdent(`weird"name`)
	if err != nil {
		t.Fatal(err)
	}
	if q != `"weird""name"` {
		t.Fatalf("%s", q)
	}
}

func TestCompareIncompatibleRegression(t *testing.T) {
	t.Parallel()
	a := models.RecallReport{WorkloadFingerprint: "aaa", Metric: "cosine", Target: "t"}
	b := models.RecallReport{WorkloadFingerprint: "bbb", Metric: "cosine", Target: "t"}
	cmp := CompareRecallReports(a, b)
	if cmp.Compatible {
		t.Fatal("expected incompatible")
	}
}
