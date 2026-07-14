// Package queries exposes read-only views of durable semantic-build records.
// The semantic payloads remain opaque JSON here; semantic packages own their
// schemas and interpret the returned payloads.
package queries

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
)

type SemanticPlan struct {
	ID            string
	ProjectID     string
	UnitID        string
	UnitNodeID    string
	ParentPlanID  string
	Lifecycle     string
	SchemaVersion string
	Payload       string
	RunID         string
	TurnID        string
	Revision      int
	CreatedAt     int64
}

type SemanticRecipe struct {
	ID              string
	ProjectID       string
	PlanRevisionID  string
	SchemaVersion   string
	TargetLanguage  string
	RendererProfile string
	Payload         string
	RunID           string
	TurnID          string
	CreatedAt       int64
}

type SemanticArtifact struct {
	ID                   string
	ProjectID            string
	PlanRevisionID       string
	RecipeID             string
	UnitNodeID           string
	Kind                 string
	ContentHash          string
	TargetLanguage       string
	TargetPath           string
	SourceRef            string
	SourceContent        string
	SourceContentAllowed bool
	RunID                string
	TurnID               string
	CreatedAt            int64
	StaleAt              int64
}

type SemanticVerification struct {
	ID, ProjectID, PlanRevisionID, RecipeID, ArtifactID, ObservedIIRID, Verdict, VerifierVersion, Payload, RunID, TurnID string
	CreatedAt                                                                                                            int64
}

type SemanticApproval struct {
	ID, ProjectID, PlanRevisionID, Scope, Decision, Rationale, ActorID, RunID, TurnID string
	CreatedAt                                                                         int64
}

type SemanticTestPlan struct {
	ID, ProjectID, PlanRevisionID, RecipeID, Payload, RunID, TurnID string
	CreatedAt                                                       int64
}

type SemanticRepair struct {
	ID, ProjectID, PlanRevisionID, RecipeID, VerificationID, Status, Payload, RunID, TurnID string
	CreatedAt                                                                               int64
}

const semanticPlanColumns = `id, project_id, unit_id, COALESCE(unit_node_id, ''), COALESCE(parent_plan_id, ''), revision, lifecycle, schema_version, payload, COALESCE(run_id, ''), COALESCE(turn_id, ''), created_at`

func LatestSemanticPlan(ctx context.Context, db *sql.DB, projectID, unitID string) (*SemanticPlan, error) {
	row := db.QueryRowContext(ctx, `SELECT `+semanticPlanColumns+` FROM semantic_plans WHERE project_id = ? AND unit_id = ? ORDER BY revision DESC, created_at DESC LIMIT 1`, projectID, unitID)
	return scanSemanticPlan(row)
}

func GetSemanticPlan(ctx context.Context, db *sql.DB, id string) (*SemanticPlan, error) {
	return scanSemanticPlan(db.QueryRowContext(ctx, `SELECT `+semanticPlanColumns+` FROM semantic_plans WHERE id = ?`, id))
}

func ListSemanticPlanHistory(ctx context.Context, db *sql.DB, projectID, unitID string) ([]SemanticPlan, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+semanticPlanColumns+` FROM semantic_plans WHERE project_id = ? AND unit_id = ? ORDER BY revision, created_at`, projectID, unitID)
	if err != nil {
		return nil, fmt.Errorf("list semantic plans: %w", err)
	}
	defer rows.Close()
	out := []SemanticPlan{}
	for rows.Next() {
		var item SemanticPlan
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.UnitID, &item.UnitNodeID, &item.ParentPlanID, &item.Revision, &item.Lifecycle, &item.SchemaVersion, &item.Payload, &item.RunID, &item.TurnID, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan semantic plan: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func ListSemanticRecipes(ctx context.Context, db *sql.DB, planID string) ([]SemanticRecipe, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, project_id, plan_revision_id, schema_version, target_language, renderer_profile, payload, COALESCE(run_id, ''), COALESCE(turn_id, ''), created_at FROM semantic_recipes WHERE plan_revision_id = ? ORDER BY created_at, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("list semantic recipes: %w", err)
	}
	defer rows.Close()
	out := []SemanticRecipe{}
	for rows.Next() {
		var item SemanticRecipe
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.PlanRevisionID, &item.SchemaVersion, &item.TargetLanguage, &item.RendererProfile, &item.Payload, &item.RunID, &item.TurnID, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan semantic recipe: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func ListSemanticArtifacts(ctx context.Context, db *sql.DB, planID string) ([]SemanticArtifact, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, project_id, plan_revision_id, recipe_id, COALESCE(unit_node_id, ''), kind, content_hash, target_language, target_path, COALESCE(source_ref, ''), COALESCE(source_content, ''), source_content_allowed, COALESCE(run_id, ''), COALESCE(turn_id, ''), created_at, COALESCE(stale_at, 0) FROM semantic_artifacts WHERE plan_revision_id = ? ORDER BY created_at, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("list semantic artifacts: %w", err)
	}
	defer rows.Close()
	out := []SemanticArtifact{}
	for rows.Next() {
		var item SemanticArtifact
		var allowed int
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.PlanRevisionID, &item.RecipeID, &item.UnitNodeID, &item.Kind, &item.ContentHash, &item.TargetLanguage, &item.TargetPath, &item.SourceRef, &item.SourceContent, &allowed, &item.RunID, &item.TurnID, &item.CreatedAt, &item.StaleAt); err != nil {
			return nil, fmt.Errorf("scan semantic artifact: %w", err)
		}
		item.SourceContentAllowed = allowed == 1
		out = append(out, item)
	}
	return out, rows.Err()
}

func ListSemanticVerifications(ctx context.Context, db *sql.DB, planID string) ([]SemanticVerification, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, project_id, plan_revision_id, recipe_id, COALESCE(artifact_id, ''), COALESCE(observed_iir_id, ''), verdict, verifier_version, payload, COALESCE(run_id, ''), COALESCE(turn_id, ''), created_at FROM semantic_verifications WHERE plan_revision_id = ? ORDER BY created_at, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("list semantic verifications: %w", err)
	}
	defer rows.Close()
	out := []SemanticVerification{}
	for rows.Next() {
		var item SemanticVerification
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.PlanRevisionID, &item.RecipeID, &item.ArtifactID, &item.ObservedIIRID, &item.Verdict, &item.VerifierVersion, &item.Payload, &item.RunID, &item.TurnID, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan semantic verification: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func ListSemanticApprovals(ctx context.Context, db *sql.DB, planID string) ([]SemanticApproval, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, project_id, plan_revision_id, scope, decision, rationale, actor_id, COALESCE(run_id, ''), COALESCE(turn_id, ''), created_at FROM semantic_approvals WHERE plan_revision_id = ? ORDER BY created_at, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("list semantic approvals: %w", err)
	}
	defer rows.Close()
	out := []SemanticApproval{}
	for rows.Next() {
		var item SemanticApproval
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.PlanRevisionID, &item.Scope, &item.Decision, &item.Rationale, &item.ActorID, &item.RunID, &item.TurnID, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan semantic approval: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func ListSemanticTestPlans(ctx context.Context, db *sql.DB, planID string) ([]SemanticTestPlan, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, project_id, plan_revision_id, recipe_id, payload, COALESCE(run_id, ''), COALESCE(turn_id, ''), created_at FROM semantic_test_plans WHERE plan_revision_id = ? ORDER BY created_at, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("list semantic test plans: %w", err)
	}
	defer rows.Close()
	out := []SemanticTestPlan{}
	for rows.Next() {
		var item SemanticTestPlan
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.PlanRevisionID, &item.RecipeID, &item.Payload, &item.RunID, &item.TurnID, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan semantic test plan: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func ListSemanticRepairs(ctx context.Context, db *sql.DB, planID string) ([]SemanticRepair, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, project_id, plan_revision_id, recipe_id, verification_id, status, payload, COALESCE(run_id, ''), COALESCE(turn_id, ''), created_at FROM semantic_repairs WHERE plan_revision_id = ? ORDER BY created_at, id`, planID)
	if err != nil {
		return nil, fmt.Errorf("list semantic repairs: %w", err)
	}
	defer rows.Close()
	out := []SemanticRepair{}
	for rows.Next() {
		var item SemanticRepair
		if err := rows.Scan(&item.ID, &item.ProjectID, &item.PlanRevisionID, &item.RecipeID, &item.VerificationID, &item.Status, &item.Payload, &item.RunID, &item.TurnID, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan semantic repair: %w", err)
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

type RecordDelta struct{ ID, Before, After string }
type VerdictDelta struct{ ArtifactID, Before, After string }
type SemanticDiff struct {
	FromPlanID, ToPlanID                                  string
	Bindings, Claims, Obligations, Decisions, RecipeSteps []RecordDelta
	VerificationVerdicts                                  []VerdictDelta
}

// DiffSemanticPlans compares stable semantic record IDs, not source text.
func DiffSemanticPlans(ctx context.Context, db *sql.DB, fromPlanID, toPlanID string) (*SemanticDiff, error) {
	from, err := GetSemanticPlan(ctx, db, fromPlanID)
	if err != nil {
		return nil, err
	}
	if from == nil {
		return nil, fmt.Errorf("semantic diff: plan %q not found", fromPlanID)
	}
	to, err := GetSemanticPlan(ctx, db, toPlanID)
	if err != nil {
		return nil, err
	}
	if to == nil {
		return nil, fmt.Errorf("semantic diff: plan %q not found", toPlanID)
	}
	diff := &SemanticDiff{FromPlanID: fromPlanID, ToPlanID: toPlanID}
	for _, field := range []struct {
		name string
		dest *[]RecordDelta
	}{{"bindings", &diff.Bindings}, {"claims", &diff.Claims}, {"obligations", &diff.Obligations}, {"decisions", &diff.Decisions}} {
		before, err := jsonRecords(from.Payload, field.name)
		if err != nil {
			return nil, err
		}
		after, err := jsonRecords(to.Payload, field.name)
		if err != nil {
			return nil, err
		}
		*field.dest = diffRecords(before, after)
	}
	fromRecipes, err := ListSemanticRecipes(ctx, db, fromPlanID)
	if err != nil {
		return nil, err
	}
	toRecipes, err := ListSemanticRecipes(ctx, db, toPlanID)
	if err != nil {
		return nil, err
	}
	var beforeSteps, afterSteps map[string]string
	if len(fromRecipes) > 0 {
		beforeSteps, err = jsonRecords(fromRecipes[len(fromRecipes)-1].Payload, "steps")
		if err != nil {
			return nil, err
		}
	}
	if len(toRecipes) > 0 {
		afterSteps, err = jsonRecords(toRecipes[len(toRecipes)-1].Payload, "steps")
		if err != nil {
			return nil, err
		}
	}
	diff.RecipeSteps = diffRecords(beforeSteps, afterSteps)
	fromVerifications, err := ListSemanticVerifications(ctx, db, fromPlanID)
	if err != nil {
		return nil, err
	}
	toVerifications, err := ListSemanticVerifications(ctx, db, toPlanID)
	if err != nil {
		return nil, err
	}
	diff.VerificationVerdicts = diffVerdicts(fromVerifications, toVerifications)
	return diff, nil
}

func UnresolvedSemanticQuestions(ctx context.Context, db *sql.DB, planID string) ([]json.RawMessage, error) {
	plan, err := GetSemanticPlan(ctx, db, planID)
	if err != nil || plan == nil {
		return nil, err
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal([]byte(plan.Payload), &payload); err != nil {
		return nil, fmt.Errorf("semantic questions: invalid payload: %w", err)
	}
	var questions []json.RawMessage
	if raw := payload["openQuestions"]; raw != nil {
		if err := json.Unmarshal(raw, &questions); err != nil {
			return nil, fmt.Errorf("semantic questions: %w", err)
		}
	}
	return questions, nil
}

func scanSemanticPlan(row *sql.Row) (*SemanticPlan, error) {
	var item SemanticPlan
	err := row.Scan(&item.ID, &item.ProjectID, &item.UnitID, &item.UnitNodeID, &item.ParentPlanID, &item.Revision, &item.Lifecycle, &item.SchemaVersion, &item.Payload, &item.RunID, &item.TurnID, &item.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan semantic plan: %w", err)
	}
	return &item, nil
}
func jsonRecords(payload, field string) (map[string]string, error) {
	out := map[string]string{}
	if payload == "" {
		return out, nil
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &root); err != nil {
		return nil, fmt.Errorf("semantic diff payload: %w", err)
	}
	var records []json.RawMessage
	if raw := root[field]; raw != nil {
		if err := json.Unmarshal(raw, &records); err != nil {
			return nil, fmt.Errorf("semantic diff %s: %w", field, err)
		}
	}
	for index, raw := range records {
		var item map[string]json.RawMessage
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, fmt.Errorf("semantic diff %s record: %w", field, err)
		}
		var decoded string
		if rawID := item["id"]; rawID != nil {
			if err := json.Unmarshal(rawID, &decoded); err != nil {
				return nil, fmt.Errorf("semantic diff %s record id: %w", field, err)
			}
		}
		if decoded == "" {
			decoded = fmt.Sprintf("%s[%d]", field, index)
		}
		out[decoded] = string(raw)
	}
	return out, nil
}
func diffRecords(before, after map[string]string) []RecordDelta {
	ids := map[string]bool{}
	for id := range before {
		ids[id] = true
	}
	for id := range after {
		ids[id] = true
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	out := []RecordDelta{}
	for _, id := range ordered {
		if !bytes.Equal([]byte(before[id]), []byte(after[id])) {
			out = append(out, RecordDelta{ID: id, Before: before[id], After: after[id]})
		}
	}
	return out
}
func diffVerdicts(before, after []SemanticVerification) []VerdictDelta {
	b, a := map[string]string{}, map[string]string{}
	for _, item := range before {
		b[item.ArtifactID] = item.Verdict
	}
	for _, item := range after {
		a[item.ArtifactID] = item.Verdict
	}
	ids := map[string]bool{}
	for id := range b {
		ids[id] = true
	}
	for id := range a {
		ids[id] = true
	}
	ordered := make([]string, 0, len(ids))
	for id := range ids {
		ordered = append(ordered, id)
	}
	sort.Strings(ordered)
	out := []VerdictDelta{}
	for _, id := range ordered {
		if b[id] != a[id] {
			out = append(out, VerdictDelta{ArtifactID: id, Before: b[id], After: a[id]})
		}
	}
	return out
}
