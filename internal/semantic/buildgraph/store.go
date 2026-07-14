// Package buildgraph persists semantic compiler artifacts through the
// substrate's typed, buffered write boundary. It contains no SQL and makes
// read-only session behavior explicit.
package buildgraph

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/atheory-ai/context-engine/internal/core"
	"github.com/atheory-ai/context-engine/internal/semantic/plan"
	"github.com/atheory-ai/context-engine/internal/semantic/recipe"
	"github.com/atheory-ai/context-engine/internal/semantic/repair"
	"github.com/atheory-ai/context-engine/internal/semantic/testplan"
	semanticverify "github.com/atheory-ai/context-engine/internal/semantic/verify"
)

var ErrReadOnly = errors.New("semantic build graph: read-scoped session cannot persist records")

type Context struct {
	RunID  core.RunID
	TurnID core.TurnID
}

type Store struct {
	writer   core.SemanticWriter
	writable bool
	now      func() int64
}

func NewStore(writer core.SemanticWriter, writable bool) *Store {
	return &Store{writer: writer, writable: writable, now: func() int64 { return time.Now().UnixMilli() }}
}

func (s *Store) PersistPlan(ctx context.Context, source *plan.SemanticPlan, meta Context) error {
	if err := s.canWrite(); err != nil {
		return err
	}
	payload, err := plan.MarshalCanonical(source)
	if err != nil {
		return fmt.Errorf("persist semantic plan: %w", err)
	}
	return s.writer.UpsertSemanticPlan(ctx, core.SemanticPlanRecord{ID: source.ID, ProjectID: source.ProjectID, UnitID: source.Unit.ID, UnitNodeID: source.Unit.NodeID, ParentPlanID: source.ParentID, Revision: source.Revision, Lifecycle: string(source.Lifecycle), SchemaVersion: source.SchemaVersion, Payload: string(payload), RunID: meta.RunID, TurnID: meta.TurnID, CreatedAt: s.now()})
}

func (s *Store) PersistRecipe(ctx context.Context, projectID core.ProjectID, lowered *recipe.ImplementationRecipe, meta Context) error {
	if err := s.canWrite(); err != nil {
		return err
	}
	payload, err := recipe.MarshalCanonical(lowered)
	if err != nil {
		return fmt.Errorf("persist semantic recipe: %w", err)
	}
	profile, err := json.Marshal(lowered.RendererProfile)
	if err != nil {
		return fmt.Errorf("persist semantic recipe profile: %w", err)
	}
	return s.writer.UpsertSemanticRecipe(ctx, core.SemanticRecipeRecord{ID: lowered.ID, ProjectID: projectID, PlanRevisionID: lowered.PlanRevisionID, SchemaVersion: lowered.SchemaVersion, TargetLanguage: lowered.TargetLanguage, RendererProfile: string(profile), Payload: string(payload), RunID: meta.RunID, TurnID: meta.TurnID, CreatedAt: s.now()})
}

func (s *Store) PersistArtifact(ctx context.Context, record core.SemanticArtifactRecord) error {
	if err := s.canWrite(); err != nil {
		return err
	}
	if record.SourceContent != "" && !record.SourceContentAllowed {
		return fmt.Errorf("persist semantic artifact: source content retention is not permitted")
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = s.now()
	}
	return s.writer.UpsertSemanticArtifact(ctx, record)
}

func (s *Store) PersistVerification(ctx context.Context, projectID core.ProjectID, id string, report *semanticverify.Report, artifactID, observedIIRID, verifierVersion string, meta Context) error {
	if err := s.canWrite(); err != nil {
		return err
	}
	if report == nil || id == "" {
		return fmt.Errorf("persist semantic verification: id and report are required")
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("persist semantic verification: %w", err)
	}
	return s.writer.RecordSemanticVerification(ctx, core.SemanticVerificationRecord{ID: id, ProjectID: projectID, PlanRevisionID: report.PlanRevisionID, RecipeID: report.RecipeID, ArtifactID: artifactID, ObservedIIRID: observedIIRID, Verdict: string(report.Status), VerifierVersion: verifierVersion, Payload: string(payload), RunID: meta.RunID, TurnID: meta.TurnID, CreatedAt: s.now()})
}

func (s *Store) PersistApproval(ctx context.Context, record core.SemanticApprovalRecord) error {
	if err := s.canWrite(); err != nil {
		return err
	}
	if record.CreatedAt == 0 {
		record.CreatedAt = s.now()
	}
	return s.writer.RecordSemanticApproval(ctx, record)
}

func (s *Store) PersistTestPlan(ctx context.Context, projectID core.ProjectID, lowered *testplan.Plan, recipeID string, meta Context) error {
	if err := s.canWrite(); err != nil {
		return err
	}
	if lowered == nil || recipeID == "" {
		return fmt.Errorf("persist semantic test plan: plan and recipe id are required")
	}
	payload, err := json.Marshal(lowered)
	if err != nil {
		return fmt.Errorf("persist semantic test plan: %w", err)
	}
	return s.writer.UpsertSemanticTestPlan(ctx, core.SemanticTestPlanRecord{ID: lowered.ID, ProjectID: projectID, PlanRevisionID: lowered.PlanRevisionID, RecipeID: recipeID, Payload: string(payload), RunID: meta.RunID, TurnID: meta.TurnID, CreatedAt: s.now()})
}

func (s *Store) PersistRepair(ctx context.Context, projectID core.ProjectID, proposed *repair.Plan, meta Context) error {
	if err := s.canWrite(); err != nil {
		return err
	}
	if proposed == nil {
		return fmt.Errorf("persist semantic repair: repair plan is required")
	}
	payload, err := json.Marshal(proposed)
	if err != nil {
		return fmt.Errorf("persist semantic repair: %w", err)
	}
	return s.writer.UpsertSemanticRepair(ctx, core.SemanticRepairRecord{ID: proposed.ID, ProjectID: projectID, PlanRevisionID: proposed.ParentPlanRevision, RecipeID: proposed.RecipeID, VerificationID: proposed.VerificationID, Status: string(proposed.Status), Payload: string(payload), RunID: meta.RunID, TurnID: meta.TurnID, CreatedAt: s.now()})
}

func (s *Store) canWrite() error {
	if !s.writable || s.writer == nil {
		return ErrReadOnly
	}
	return nil
}
