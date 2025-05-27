package collector

import (
	"context"

	"github.com/hghukasyan/pgscope/internal/vector"
	"github.com/hghukasyan/pgscope/pkg/models"
)

func (c *Collector) collectVector(ctx context.Context) (models.VectorInventory, []models.Limitation) {
	inv, err := vector.Discover(ctx, c.pool, vector.DiscoverOptions{})
	if err != nil {
		return models.VectorInventory{}, []models.Limitation{{
			Area: "pgvector", Reason: err.Error(), Impact: "Vector discovery unavailable",
		}}
	}
	lims := inv.Limitations
	inv.Limitations = nil
	return inv, lims
}
