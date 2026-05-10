// Package ontology manages the concept vocabulary for the Context Engine.
// Concept seeds are pre-loaded vocabulary entries that improve pre-flight
// recognition and anchor resolution during the cognitive loop.
package ontology

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/storage/writebuffer"
)

// ConceptSeedRow is the raw concept seed as stored in the database.
type ConceptSeedRow struct {
	ID         string
	Term       string
	Scope      string // project | org
	Definition string
	Related    string // JSON array
	Synonyms   string // JSON array
	Source     string
	PluginID   string
	CreatedAt  int64
	UpdatedAt  int64
}

// Ontology manages concept seed lookups and normalization.
type Ontology struct {
	dbProvider writebuffer.DBProvider
	buffer     writebuffer.Buffer
}

// New creates an Ontology backed by the given DBProvider and write buffer.
func New(dbProvider writebuffer.DBProvider, buf writebuffer.Buffer) *Ontology {
	return &Ontology{
		dbProvider: dbProvider,
		buffer:     buf,
	}
}

// LookupTerm looks up a concept seed by its normalized term in a project's graph.
// Returns nil if not found.
func (o *Ontology) LookupTerm(ctx context.Context, projectID, term string) (*ConceptSeedRow, error) {
	db, err := o.dbProvider.GraphDB(projectID)
	if err != nil {
		return nil, fmt.Errorf("graph db for project %s: %w", projectID, err)
	}
	return lookupTerm(ctx, db, NormalizeTerm(term))
}

// LookupTermOrg looks up a concept seed in the org-level graph.
func (o *Ontology) LookupTermOrg(ctx context.Context, term string) (*ConceptSeedRow, error) {
	db, err := o.dbProvider.GraphDB("org")
	if err != nil {
		return nil, fmt.Errorf("org graph db: %w", err)
	}
	return lookupTerm(ctx, db, NormalizeTerm(term))
}

// SeedConcepts upserts concept seeds for a project via the write buffer.
// Source is "manual" for user-defined seeds, "plugin" for plugin-contributed seeds.
func (o *Ontology) SeedConcepts(ctx context.Context, projectID string, seeds []core.ConceptSeed, source, pluginID string) error {
	for _, seed := range seeds {
		related, err := marshalStringSlice(seed.Related)
		if err != nil {
			return fmt.Errorf("marshal related for %q: %w", seed.Term, err)
		}
		synonyms, err := marshalStringSlice(seed.Synonyms)
		if err != nil {
			return fmt.Errorf("marshal synonyms for %q: %w", seed.Term, err)
		}

		normalTerm := NormalizeTerm(seed.Term)
		id := core.MakeNodeID(projectID, "concept_seed", normalTerm)
		now := time.Now().UnixMilli()

		if err := o.buffer.Send(writebuffer.WriteOp{
			Type:      writebuffer.OpUpsertConcept,
			ProjectID: projectID,
			Payload: writebuffer.ConceptUpsert{
				ID:         id,
				Term:       normalTerm,
				Scope:      scopeFor(projectID),
				Definition: seed.Definition,
				Related:    related,
				Synonyms:   synonyms,
				Source:     source,
				PluginID:   pluginID,
				CreatedAt:  now,
				UpdatedAt:  now,
			},
		}); err != nil {
			return fmt.Errorf("queue concept seed %q: %w", seed.Term, err)
		}
	}
	return nil
}

// NormalizeTerm normalizes a concept term for consistent storage and lookup.
// Rules: lowercase, trim whitespace, collapse internal spaces to single space.
func NormalizeTerm(term string) string {
	term = strings.ToLower(strings.TrimSpace(term))
	var prev rune
	var b strings.Builder
	for _, r := range term {
		if unicode.IsSpace(r) {
			if prev != ' ' {
				b.WriteRune(' ')
			}
		} else {
			b.WriteRune(r)
		}
		prev = r
	}
	return b.String()
}

// ============================================================
// Internal helpers
// ============================================================

func lookupTerm(ctx context.Context, db *sql.DB, normalTerm string) (*ConceptSeedRow, error) {
	var c ConceptSeedRow
	err := db.QueryRowContext(ctx, `
		SELECT id, term, scope, COALESCE(definition, ''), related, synonyms,
		       source, COALESCE(plugin_id, ''), created_at, updated_at
		FROM concept_seeds WHERE term = ?
	`, normalTerm).Scan(
		&c.ID, &c.Term, &c.Scope, &c.Definition, &c.Related, &c.Synonyms,
		&c.Source, &c.PluginID, &c.CreatedAt, &c.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lookup term %q: %w", normalTerm, err)
	}
	return &c, nil
}

func marshalStringSlice(s []string) (string, error) {
	if len(s) == 0 {
		return "[]", nil
	}
	var b strings.Builder
	b.WriteString("[")
	for i, v := range s {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(fmt.Sprintf("%q", v))
	}
	b.WriteString("]")
	return b.String(), nil
}

func scopeFor(projectID string) string {
	if projectID == "org" {
		return "org"
	}
	return "project"
}
