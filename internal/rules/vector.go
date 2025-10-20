package rules

import (
	"context"

	"github.com/hghukasyan/pgscope/internal/vector"
	"github.com/hghukasyan/pgscope/pkg/models"
)

// VectorInventoryRule evaluates discovered pgvector inventory.
type VectorInventoryRule struct{}

func (VectorInventoryRule) ID() string { return "VEC.INVENTORY" }

func (VectorInventoryRule) Evaluate(_ context.Context, snap models.Snapshot) ([]models.Finding, error) {
	if !snap.Vector.Collected || !snap.Vector.Available {
		return nil, nil
	}
	return vector.AnalyzeInventory(snap.Vector, snap.Instance.Database), nil
}
