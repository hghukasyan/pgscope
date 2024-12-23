package collector

import (
	"testing"

	"github.com/hghukasyan/pgscope/pkg/models"
)

func TestFindDuplicateIndexes(t *testing.T) {
	t.Parallel()
	a := models.IndexInfo{
		Schema: "public", Name: "a_idx", TableSchema: "public", TableName: "t",
		AccessMethod: "btree", IndexColumns: []string{"x"}, IsValid: true,
	}
	b := a
	b.Name = "b_idx"
	a.StructuralKey = structuralKey(a)
	b.StructuralKey = structuralKey(b)
	groups := FindDuplicateIndexes([]models.IndexInfo{a, b})
	if len(groups) != 1 {
		t.Fatalf("groups=%d", len(groups))
	}
}

func TestPartialIndexesNotDuplicateAcrossPredicates(t *testing.T) {
	t.Parallel()
	a := models.IndexInfo{
		Schema: "public", Name: "a", TableSchema: "public", TableName: "t",
		AccessMethod: "btree", IndexColumns: []string{"x"}, Predicate: "y = 1", IsPartial: true, IsValid: true,
	}
	b := a
	b.Name = "b"
	b.Predicate = "y = 2"
	a.StructuralKey = structuralKey(a)
	b.StructuralKey = structuralKey(b)
	if len(FindDuplicateIndexes([]models.IndexInfo{a, b})) != 0 {
		t.Fatal("different predicates must not be duplicates")
	}
}

func TestRedundantBTreePrefix(t *testing.T) {
	t.Parallel()
	wide := models.IndexInfo{
		Schema: "public", Name: "wide", TableSchema: "public", TableName: "orders",
		AccessMethod: "btree", IndexColumns: []string{"a", "b"}, OpClasses: []string{"int4_ops", "int4_ops"},
		IndOption: []int16{0, 0}, IsValid: true,
	}
	narrow := models.IndexInfo{
		Schema: "public", Name: "narrow", TableSchema: "public", TableName: "orders",
		AccessMethod: "btree", IndexColumns: []string{"a"}, OpClasses: []string{"int4_ops"},
		IndOption: []int16{0}, IsValid: true,
	}
	groups := FindRedundantBTreeIndexes([]models.IndexInfo{wide, narrow})
	if len(groups) == 0 {
		t.Fatal("expected redundant prefix group")
	}
}

func TestUniqueNarrowNotRedundantWithNonUniqueWide(t *testing.T) {
	t.Parallel()
	wide := models.IndexInfo{
		Schema: "public", Name: "wide", TableSchema: "public", TableName: "orders",
		AccessMethod: "btree", IndexColumns: []string{"a", "b"}, IsValid: true,
	}
	narrow := models.IndexInfo{
		Schema: "public", Name: "narrow", TableSchema: "public", TableName: "orders",
		AccessMethod: "btree", IndexColumns: []string{"a"}, IsUnique: true, IsValid: true,
	}
	if len(FindRedundantBTreeIndexes([]models.IndexInfo{wide, narrow})) != 0 {
		t.Fatal("unique narrower index must not be marked redundant vs non-unique wider")
	}
}

func TestConstraintBackedNotRedundant(t *testing.T) {
	t.Parallel()
	wide := models.IndexInfo{
		Schema: "public", Name: "wide", TableSchema: "public", TableName: "orders",
		AccessMethod: "btree", IndexColumns: []string{"a", "b"}, IsValid: true,
	}
	pk := models.IndexInfo{
		Schema: "public", Name: "orders_pkey", TableSchema: "public", TableName: "orders",
		AccessMethod: "btree", IndexColumns: []string{"a"}, IsPrimary: true, ConstraintName: "orders_pkey", IsValid: true,
	}
	if len(FindRedundantBTreeIndexes([]models.IndexInfo{wide, pk})) != 0 {
		t.Fatal("PK must not be redundant candidate")
	}
}
