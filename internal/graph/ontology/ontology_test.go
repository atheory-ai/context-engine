package ontology_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/atheory-ai/context-engine/internal/graph/ontology"
	"github.com/atheory-ai/context-engine/internal/storage/db"
	"github.com/atheory-ai/context-engine/internal/storage/migrations"
)

type provider struct{ db *sql.DB }

func (p provider) GraphDB(string) (*sql.DB, error) { return p.db, nil }

func TestNormalizeTerm(t *testing.T) {
	cases := map[string]string{
		"  Payment   Gateway ": "payment gateway",
		"UPPER":                "upper",
		"a\tb\nc":              "a b c",
		"already normal":       "already normal",
	}
	for in, want := range cases {
		if got := ontology.NormalizeTerm(in); got != want {
			t.Errorf("NormalizeTerm(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLookupTerm_RoundTrip(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	if err := migrations.RunGraph(d); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := d.Exec(`INSERT INTO concept_seeds (id, term, scope, definition, source, created_at, updated_at)
		VALUES ('c1', 'payment gateway', 'project', 'processes payments', 'manual', 1, 1)`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	o := ontology.New(provider{db: d}, nil)
	ctx := context.Background()

	// Lookup normalizes the query, so a messy-cased term still matches.
	got, err := o.LookupTerm(ctx, "proj", "  Payment   Gateway ")
	if err != nil {
		t.Fatalf("LookupTerm: %v", err)
	}
	if got == nil || got.Term != "payment gateway" || got.Definition != "processes payments" {
		t.Fatalf("lookup = %+v", got)
	}

	// A miss returns (nil, nil), not an error.
	miss, err := o.LookupTerm(ctx, "proj", "nonexistent")
	if err != nil {
		t.Fatalf("LookupTerm miss: %v", err)
	}
	if miss != nil {
		t.Errorf("expected nil for missing term, got %+v", miss)
	}
}
