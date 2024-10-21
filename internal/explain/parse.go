// Package explain parses PostgreSQL EXPLAIN (FORMAT JSON) output and emits
// evidence-backed plan findings.
package explain

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hghukasyan/pgscope/pkg/models"
)

type explainEnvelope struct {
	Plan           json.RawMessage `json:"Plan"`
	PlanningTime   *float64        `json:"Planning Time"`
	ExecutionTime  *float64        `json:"Execution Time"`
}

type rawNode map[string]any

// ParseJSON parses EXPLAIN (FORMAT JSON) output into an ExplainResult skeleton
// (without diagnostic findings).
func ParseJSON(raw []byte) (models.ExplainResult, error) {
	var out models.ExplainResult
	raw = trimJSON(raw)
	if len(raw) == 0 {
		return out, fmt.Errorf("empty EXPLAIN JSON")
	}

	// PostgreSQL returns a JSON array with one object.
	var arr []explainEnvelope
	if err := json.Unmarshal(raw, &arr); err != nil {
		// Also accept a bare object for flexibility.
		var one explainEnvelope
		if err2 := json.Unmarshal(raw, &one); err2 != nil {
			return out, fmt.Errorf("parse EXPLAIN JSON: %w", err)
		}
		arr = []explainEnvelope{one}
	}
	if len(arr) == 0 {
		return out, fmt.Errorf("EXPLAIN JSON array is empty")
	}
	env := arr[0]
	out.PlanningTimeMS = env.PlanningTime
	out.ExecutionTimeMS = env.ExecutionTime
	if env.ExecutionTime != nil {
		out.Analyze = true
	}

	var root rawNode
	if err := json.Unmarshal(env.Plan, &root); err != nil {
		return out, fmt.Errorf("parse Plan node: %w", err)
	}
	out.Root = convertNode(root, "0")
	out.Summary = summarize(out.Root)
	return out, nil
}

func trimJSON(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func convertNode(m rawNode, path string) models.PlanNode {
	n := models.PlanNode{
		Path:               path,
		NodeType:           str(m, "Node Type"),
		ParentRelationship: str(m, "Parent Relationship"),
		RelationName:       str(m, "Relation Name"),
		Schema:             str(m, "Schema"),
		Alias:              str(m, "Alias"),
		IndexName:          str(m, "Index Name"),
		IndexCond:          str(m, "Index Cond"),
		Filter:             str(m, "Filter"),
		JoinFilter:         str(m, "Join Filter"),
		HashCond:           str(m, "Hash Cond"),
		MergeCond:          str(m, "Merge Cond"),
		RecheckCond:        str(m, "Recheck Cond"),
		StartupCost:        f64(m, "Startup Cost"),
		TotalCost:          f64(m, "Total Cost"),
		PlanRows:           f64(m, "Plan Rows"),
		PlanWidth:          int(f64(m, "Plan Width")),
		SortMethod:         str(m, "Sort Method"),
		SortSpaceType:      str(m, "Sort Space Type"),
		ParallelAware:      boolVal(m, "Parallel Aware"),
		ParallelSafe:       boolVal(m, "Parallel Safe"),
	}
	n.ActualStartupTime = f64Ptr(m, "Actual Startup Time")
	n.ActualTotalTime = f64Ptr(m, "Actual Total Time")
	n.ActualRows = f64Ptr(m, "Actual Rows")
	n.ActualLoops = f64Ptr(m, "Actual Loops")
	n.SharedHitBlocks = i64Ptr(m, "Shared Hit Blocks")
	n.SharedReadBlocks = i64Ptr(m, "Shared Read Blocks")
	n.SharedDirtiedBlocks = i64Ptr(m, "Shared Dirtied Blocks")
	n.SharedWrittenBlocks = i64Ptr(m, "Shared Written Blocks")
	n.TempReadBlocks = i64Ptr(m, "Temp Read Blocks")
	n.TempWrittenBlocks = i64Ptr(m, "Temp Written Blocks")
	n.SortSpaceUsedKB = f64Ptr(m, "Sort Space Used")
	n.HashBatches = i64Ptr(m, "Hash Batches")
	n.HashBuckets = i64Ptr(m, "Hash Buckets")
	n.PeakMemoryKB = f64Ptr(m, "Peak Memory Usage")
	if v, ok := m["Workers Planned"]; ok {
		n.WorkersPlanned = intPtr(v)
	}
	if v, ok := m["Workers Launched"]; ok {
		n.WorkersLaunched = intPtr(v)
	}
	if outs, ok := m["Output"].([]any); ok {
		for _, o := range outs {
			if s, ok := o.(string); ok {
				n.Output = append(n.Output, s)
			}
		}
	}

	known := map[string]struct{}{
		"Node Type": {}, "Parent Relationship": {}, "Relation Name": {}, "Schema": {},
		"Alias": {}, "Index Name": {}, "Index Cond": {}, "Filter": {}, "Join Filter": {},
		"Hash Cond": {}, "Merge Cond": {}, "Recheck Cond": {}, "Startup Cost": {},
		"Total Cost": {}, "Plan Rows": {}, "Plan Width": {}, "Actual Startup Time": {},
		"Actual Total Time": {}, "Actual Rows": {}, "Actual Loops": {},
		"Shared Hit Blocks": {}, "Shared Read Blocks": {}, "Shared Dirtied Blocks": {},
		"Shared Written Blocks": {}, "Temp Read Blocks": {}, "Temp Written Blocks": {},
		"Sort Method": {}, "Sort Space Type": {}, "Sort Space Used": {},
		"Hash Batches": {}, "Hash Buckets": {}, "Peak Memory Usage": {},
		"Workers Planned": {}, "Workers Launched": {}, "Parallel Aware": {},
		"Parallel Safe": {}, "Output": {}, "Plans": {},
	}
	extra := map[string]any{}
	for k, v := range m {
		if _, ok := known[k]; ok {
			continue
		}
		extra[k] = v
	}
	if len(extra) > 0 {
		n.Extra = extra
	}

	if plans, ok := m["Plans"].([]any); ok {
		for i, p := range plans {
			childMap, ok := p.(map[string]any)
			if !ok {
				continue
			}
			n.Plans = append(n.Plans, convertNode(rawNode(childMap), fmt.Sprintf("%s.%d", path, i)))
		}
	}
	return n
}

func summarize(root models.PlanNode) models.PlanSummary {
	var s models.PlanSummary
	var walk func(models.PlanNode)
	walk = func(n models.PlanNode) {
		s.NodeCount++
		if n.TotalCost > s.TotalEstimatedCost {
			s.TotalEstimatedCost = n.TotalCost
		}
		if n.PlanRows > s.MaxEstimatedRows {
			s.MaxEstimatedRows = n.PlanRows
		}
		if n.RelationName != "" {
			rel := n.RelationName
			if n.Schema != "" {
				rel = n.Schema + "." + n.RelationName
			}
			s.Relations = appendUnique(s.Relations, rel)
		}
		nt := strings.ToLower(n.NodeType)
		switch {
		case nt == "seq scan":
			s.SeqScans++
		case nt == "index scan":
			s.IndexScans++
		case nt == "index only scan":
			s.IndexOnlyScans++
		case strings.Contains(nt, "bitmap"):
			s.BitmapScans++
		case nt == "nested loop":
			s.NestedLoops++
		case nt == "hash join":
			s.HashJoins++
		case nt == "merge join":
			s.MergeJoins++
		case nt == "sort":
			s.Sorts++
			if strings.EqualFold(n.SortSpaceType, "Disk") {
				s.HasDiskSort = true
			}
		case strings.Contains(nt, "aggregate"):
			s.Aggregates++
		case nt == "materialize":
			s.Materialize++
		}
		if n.ParallelAware || (n.WorkersLaunched != nil && *n.WorkersLaunched > 0) {
			s.ParallelNodes++
		}
		if n.HashBatches != nil && n.HashBuckets != nil && *n.HashBatches > *n.HashBuckets && *n.HashBuckets > 0 {
			s.HasHashSpill = true
		}
		// Alternative spill signal: Hash Batches > 1 often indicates spill.
		if n.HashBatches != nil && *n.HashBatches > 1 {
			if v, ok := n.Extra["Original Hash Batches"]; ok {
				if orig, ok := asFloat(v); ok && float64(*n.HashBatches) > orig {
					s.HasHashSpill = true
				}
			} else {
				s.HasHashSpill = true
			}
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	walk(root)
	return s
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func str(m rawNode, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func f64(m rawNode, key string) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	f, _ := asFloat(v)
	return f
}

func f64Ptr(m rawNode, key string) *float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	f, ok := asFloat(v)
	if !ok {
		return nil
	}
	return &f
}

func i64Ptr(m rawNode, key string) *int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	f, ok := asFloat(v)
	if !ok {
		return nil
	}
	i := int64(f)
	return &i
}

func boolVal(m rawNode, key string) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func intPtr(v any) *int {
	f, ok := asFloat(v)
	if !ok {
		return nil
	}
	i := int(f)
	return &i
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}
