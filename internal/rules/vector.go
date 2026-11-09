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
	inv := snap.Vector
	inv.Settings = mergeSettings(inv.Settings, snap.Instance.Settings, "shared_buffers", "effective_cache_size")
	return vector.AnalyzeInventory(inv, snap.Instance.Database), nil
}

func mergeSettings(dst, src map[string]string, keys ...string) map[string]string {
	if len(keys) == 0 {
		return dst
	}
	out := dst
	if out == nil {
		out = map[string]string{}
	} else {
		cp := make(map[string]string, len(out)+len(keys))
		for k, v := range out {
			cp[k] = v
		}
		out = cp
	}
	for _, k := range keys {
		if out[k] != "" {
			continue
		}
		if src == nil {
			continue
		}
		if v := src[k]; v != "" {
			out[k] = v
		}
	}
	return out
}
