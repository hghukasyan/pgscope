package sqlsafe

import "testing"

func TestValidateReadOnlySelectOK(t *testing.T) {
	t.Parallel()
	cases := []string{
		"SELECT 1",
		"SELECT * FROM orders WHERE id = 1",
		"WITH c AS (SELECT 1 AS x) SELECT * FROM c",
		"VALUES (1), (2)",
		"TABLE users",
		"SELECT $$semi;colon$$ AS s",
		"SELECT 'a;b' AS s",
		"-- comment\nSELECT 1",
		"SELECT /* ; */ 1",
	}
	for _, sql := range cases {
		res, err := ValidateReadOnlySelect(sql)
		if err != nil {
			t.Fatalf("%q: %v", sql, err)
		}
		if res.Class != ClassSelect {
			t.Fatalf("%q: class %s", sql, res.Class)
		}
	}
}

func TestValidateRejectsUnsafe(t *testing.T) {
	t.Parallel()
	cases := []string{
		"SELECT 1; SELECT 2",
		"DELETE FROM orders",
		"UPDATE orders SET x=1",
		"INSERT INTO t VALUES (1)",
		"DROP TABLE orders",
		"WITH x AS (DELETE FROM t RETURNING *) SELECT * FROM x",
		"WITH x AS (INSERT INTO t VALUES (1) RETURNING *) SELECT * FROM x",
		"SELECT INTO t FROM orders",
		"COPY orders TO STDOUT",
		"DO $$ BEGIN NULL; END $$",
		"CREATE TABLE t (id int)",
		"",
	}
	for _, sql := range cases {
		if _, err := ValidateReadOnlySelect(sql); err == nil {
			t.Fatalf("expected rejection for %q", sql)
		}
	}
}

func TestValidateExplainAnalyzeSideEffects(t *testing.T) {
	t.Parallel()
	_, err := ValidateExplainAnalyze("SELECT pg_sleep(1)", false)
	if err == nil {
		t.Fatal("expected side-effect rejection")
	}
	if _, err := ValidateExplainAnalyze("SELECT pg_sleep(1)", true); err != nil {
		t.Fatalf("trusted override should allow: %v", err)
	}
}

func TestDollarQuoteAndComments(t *testing.T) {
	t.Parallel()
	sql := `SELECT $tag$SELECT 1; DELETE FROM t$tag$ AS body`
	if _, err := ValidateReadOnlySelect(sql); err != nil {
		t.Fatal(err)
	}
}

func TestSelectColumnNamedSetAllowed(t *testing.T) {
	t.Parallel()
	if _, err := ValidateReadOnlySelect(`SELECT set FROM t`); err != nil {
		t.Fatal(err)
	}
}
