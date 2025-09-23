package vector

import (
	"fmt"

	"github.com/hghukasyan/pgscope/pkg/models"
)

// EstimateStorage produces clearly labeled storage estimates from inventory metadata.
// Measured table/index sizes come from PostgreSQL; payload projections are estimates.
func EstimateStorage(inv models.VectorInventory) models.VectorStorageReport {
	rep := models.VectorStorageReport{
		Collected: inv.Collected && inv.Available,
		Notes:     "estimated_* fields are projections using type width assumptions (not measured TOAST/physical layout). measured_* fields come from pg_relation_size.",
	}
	if !rep.Collected {
		return rep
	}

	indexBytesByTableCol := map[string]int64{}
	for _, idx := range inv.Indexes {
		for _, col := range idx.IndexedColumns {
			key := idx.TableSchema + "." + idx.TableName + "." + col
			indexBytesByTableCol[key] += idx.SizeBytes
		}
	}

	for _, col := range inv.Columns {
		est := models.VectorStorageEstimate{
			Schema:             col.Schema,
			Table:              col.Table,
			Column:             col.Column,
			TypeName:           col.TypeName,
			Dimensions:         col.Dimensions,
			EstimatedRows:      col.EstimatedRows,
			MeasuredTableBytes: col.TableBytes,
			IsEstimate:         true,
			Notes:              "Vector payload estimate assumes dense float storage plus modest per-row overhead; excludes shared table headers and compression effects.",
		}
		key := col.Schema + "." + col.Table + "." + col.Column
		est.MeasuredIndexBytes = indexBytesByTableCol[key]

		if col.Dimensions != nil && col.EstimatedRows > 0 {
			dims := int64(*col.Dimensions)
			switch col.TypeName {
			case "vector":
				// 4 bytes/dim + ~8 byte header-ish allowance per value (estimate).
				est.EstVectorPayloadB = col.EstimatedRows * (dims*4 + 8)
				est.EstHalfvecPayloadB = col.EstimatedRows * (dims*2 + 8)
			case "halfvec":
				est.EstVectorPayloadB = col.EstimatedRows * (dims*2 + 8)
				est.EstHalfvecPayloadB = est.EstVectorPayloadB
			case "sparsevec":
				// Highly variable; mark as unknown payload with note.
				est.Notes = "sparsevec payload is data-dependent; only measured table/index sizes are reliable."
			}
			// HNSW graph overhead rough: often multiple of payload; label clearly.
			if est.MeasuredIndexBytes > 0 {
				est.EstIndexOverheadB = est.MeasuredIndexBytes
				est.Notes += " Index overhead uses measured index size (preferred over geometric guesses)."
			} else if est.EstVectorPayloadB > 0 {
				est.EstIndexOverheadB = est.EstVectorPayloadB // rough placeholder labeled as estimate
				est.Notes += " Without an index, index overhead is a rough payload-sized placeholder only."
			}
		} else {
			est.Notes = "Dimensions unconstrained or row estimate unavailable; measured sizes only."
		}
		rep.Estimates = append(rep.Estimates, est)
	}
	return rep
}

// FormatStorageBytes is a helper for CLI.
func FormatStorageBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
