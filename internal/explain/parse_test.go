package explain

import (
	"testing"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestParseNestedPlan(t *testing.T) {
	t.Parallel()
	raw := []byte(`[
	  {
	    "Plan": {
	      "Node Type": "Nested Loop",
	      "Startup Cost": 0.0,
	      "Total Cost": 100.0,
	      "Plan Rows": 1000,
	      "Plans": [
	        {
	          "Node Type": "Seq Scan",
	          "Relation Name": "orders",
	          "Schema": "public",
	          "Filter": "(status = 'open')",
	          "Startup Cost": 0.0,
	          "Total Cost": 50.0,
	          "Plan Rows": 100000,
	          "Rows Removed by Filter": 500000
	        },
	        {
	          "Node Type": "Index Scan",
	          "Relation Name": "items",
	          "Index Name": "items_pkey",
	          "Startup Cost": 0.0,
	          "Total Cost": 8.0,
	          "Plan Rows": 1
	        }
	      ]
	    }
	  }
	]`)
	res, err := ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if res.Root.NodeType != "Nested Loop" {
		t.Fatalf("root=%s", res.Root.NodeType)
	}
	if res.Summary.NodeCount != 3 {
		t.Fatalf("nodes=%d", res.Summary.NodeCount)
	}
	if res.Summary.SeqScans != 1 || res.Summary.NestedLoops != 1 || res.Summary.IndexScans != 1 {
		t.Fatalf("summary=%+v", res.Summary)
	}
	AnalyzePlan(&res, "app")
	if len(res.Findings) == 0 {
		t.Fatal("expected findings for large filtered seq scan / nested loop")
	}
}

func TestParseJSONError(t *testing.T) {
	t.Parallel()
	if _, err := ParseJSON([]byte(`{not json`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestCardinalityMismatch(t *testing.T) {
	t.Parallel()
	actualRows := 100000.0
	loops := 1.0
	n := models.PlanNode{
		NodeType: "Seq Scan", Path: "0", PlanRows: 100, ActualRows: &actualRows, ActualLoops: &loops,
	}
	res := models.ExplainResult{Root: n, Analyze: true}
	AnalyzePlan(&res, "db")
	found := false
	for _, f := range res.Findings {
		if f.RuleID == "EXPLAIN.CARDINALITY" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected cardinality finding, got %+v", res.Findings)
	}
}

func TestSortSpillDetection(t *testing.T) {
	t.Parallel()
	used := 1024.0
	n := models.PlanNode{
		NodeType: "Sort", Path: "0.1", SortMethod: "external merge", SortSpaceType: "Disk", SortSpaceUsedKB: &used,
	}
	res := models.ExplainResult{Root: n, Analyze: true}
	AnalyzePlan(&res, "db")
	found := false
	for _, f := range res.Findings {
		if f.RuleID == "EXPLAIN.SORT_SPILL" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected sort spill finding: %+v", res.Findings)
	}
}

func TestSmallSeqScanNotFlagged(t *testing.T) {
	t.Parallel()
	n := models.PlanNode{NodeType: "Seq Scan", Path: "0", RelationName: "tiny", PlanRows: 10, TotalCost: 1.0}
	res := models.ExplainResult{Root: n, Summary: summarize(n)}
	AnalyzePlan(&res, "db")
	for _, f := range res.Findings {
		if f.RuleID == "EXPLAIN.LARGE_SEQ_SCAN" {
			t.Fatalf("small seq scan should not be flagged")
		}
	}
}
