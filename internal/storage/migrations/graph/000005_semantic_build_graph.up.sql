-- Durable semantic-development artifacts. Payload columns hold canonical JSON
-- owned by internal/semantic; relational columns provide safe linkage and
-- queryability without duplicating those models into the substrate package.

CREATE TABLE IF NOT EXISTS semantic_plans (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL,
    unit_id         TEXT NOT NULL,
    unit_node_id    TEXT REFERENCES nodes(id) ON DELETE SET NULL,
    parent_plan_id  TEXT REFERENCES semantic_plans(id) ON DELETE RESTRICT,
    revision        INTEGER NOT NULL CHECK (revision > 0),
    lifecycle       TEXT NOT NULL CHECK (lifecycle IN ('declared', 'shaping', 'resolved', 'blocked')),
    schema_version  TEXT NOT NULL CHECK (schema_version = 'v1'),
    payload         TEXT NOT NULL CHECK (json_valid(payload)),
    run_id          TEXT,
    turn_id         TEXT,
    created_at      INTEGER NOT NULL,
    UNIQUE (project_id, unit_id, revision)
);
CREATE INDEX IF NOT EXISTS idx_semantic_plans_unit ON semantic_plans(project_id, unit_id, revision DESC);
CREATE INDEX IF NOT EXISTS idx_semantic_plans_node ON semantic_plans(project_id, unit_node_id);
CREATE INDEX IF NOT EXISTS idx_semantic_plans_parent ON semantic_plans(parent_plan_id);

CREATE TABLE IF NOT EXISTS semantic_recipes (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL,
    plan_revision_id  TEXT NOT NULL REFERENCES semantic_plans(id) ON DELETE RESTRICT,
    schema_version    TEXT NOT NULL CHECK (schema_version = 'v1'),
    target_language   TEXT NOT NULL,
    renderer_profile  TEXT NOT NULL CHECK (json_valid(renderer_profile)),
    payload           TEXT NOT NULL CHECK (json_valid(payload)),
    run_id            TEXT,
    turn_id           TEXT,
    created_at        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_semantic_recipes_plan ON semantic_recipes(plan_revision_id, created_at);

CREATE TABLE IF NOT EXISTS semantic_artifacts (
    id                      TEXT PRIMARY KEY,
    project_id              TEXT NOT NULL,
    plan_revision_id        TEXT NOT NULL REFERENCES semantic_plans(id) ON DELETE RESTRICT,
    recipe_id               TEXT NOT NULL REFERENCES semantic_recipes(id) ON DELETE RESTRICT,
    unit_node_id            TEXT REFERENCES nodes(id) ON DELETE SET NULL,
    kind                    TEXT NOT NULL CHECK (kind IN ('source', 'test', 'analysis')),
    content_hash            TEXT NOT NULL,
    target_language         TEXT NOT NULL,
    target_path             TEXT NOT NULL,
    source_ref              TEXT,
    source_content          TEXT,
    source_content_allowed  INTEGER NOT NULL DEFAULT 0 CHECK (source_content_allowed IN (0, 1)),
    run_id                  TEXT,
    turn_id                 TEXT,
    created_at              INTEGER NOT NULL,
    stale_at                INTEGER,
    CHECK (source_content IS NULL OR source_content_allowed = 1),
    UNIQUE (recipe_id, kind, content_hash, target_path)
);
CREATE INDEX IF NOT EXISTS idx_semantic_artifacts_plan ON semantic_artifacts(plan_revision_id, created_at);
CREATE INDEX IF NOT EXISTS idx_semantic_artifacts_node ON semantic_artifacts(project_id, unit_node_id);
CREATE INDEX IF NOT EXISTS idx_semantic_artifacts_recipe ON semantic_artifacts(recipe_id);
CREATE INDEX IF NOT EXISTS idx_semantic_artifacts_stale ON semantic_artifacts(project_id, stale_at);

CREATE TABLE IF NOT EXISTS semantic_verifications (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL,
    plan_revision_id  TEXT NOT NULL REFERENCES semantic_plans(id) ON DELETE RESTRICT,
    recipe_id         TEXT NOT NULL REFERENCES semantic_recipes(id) ON DELETE RESTRICT,
    artifact_id       TEXT REFERENCES semantic_artifacts(id) ON DELETE SET NULL,
    observed_iir_id   TEXT REFERENCES iir(id) ON DELETE SET NULL,
    verdict           TEXT NOT NULL CHECK (verdict IN ('passed', 'failed', 'inconclusive')),
    verifier_version  TEXT NOT NULL,
    payload           TEXT NOT NULL CHECK (json_valid(payload)),
    run_id            TEXT,
    turn_id           TEXT,
    created_at        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_semantic_verifications_plan ON semantic_verifications(plan_revision_id, created_at);
CREATE INDEX IF NOT EXISTS idx_semantic_verifications_artifact ON semantic_verifications(artifact_id, created_at);
CREATE INDEX IF NOT EXISTS idx_semantic_verifications_verdict ON semantic_verifications(project_id, verdict, created_at);

CREATE TABLE IF NOT EXISTS semantic_approvals (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL,
    plan_revision_id  TEXT NOT NULL REFERENCES semantic_plans(id) ON DELETE RESTRICT,
    scope             TEXT NOT NULL,
    decision          TEXT NOT NULL CHECK (decision IN ('approved', 'rejected', 'waived')),
    rationale         TEXT NOT NULL,
    actor_id          TEXT NOT NULL,
    run_id            TEXT,
    turn_id           TEXT,
    created_at        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_semantic_approvals_plan ON semantic_approvals(plan_revision_id, created_at);

CREATE TABLE IF NOT EXISTS semantic_test_plans (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL,
    plan_revision_id  TEXT NOT NULL REFERENCES semantic_plans(id) ON DELETE RESTRICT,
    recipe_id         TEXT NOT NULL REFERENCES semantic_recipes(id) ON DELETE RESTRICT,
    payload           TEXT NOT NULL CHECK (json_valid(payload)),
    run_id            TEXT,
    turn_id           TEXT,
    created_at        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_semantic_test_plans_plan ON semantic_test_plans(plan_revision_id, created_at);

CREATE TABLE IF NOT EXISTS semantic_repairs (
    id                TEXT PRIMARY KEY,
    project_id        TEXT NOT NULL,
    plan_revision_id  TEXT NOT NULL REFERENCES semantic_plans(id) ON DELETE RESTRICT,
    recipe_id         TEXT NOT NULL REFERENCES semantic_recipes(id) ON DELETE RESTRICT,
    verification_id   TEXT NOT NULL REFERENCES semantic_verifications(id) ON DELETE RESTRICT,
    status            TEXT NOT NULL CHECK (status IN ('proposed', 'approved', 'applied', 'rejected', 'exhausted')),
    payload           TEXT NOT NULL CHECK (json_valid(payload)),
    run_id            TEXT,
    turn_id           TEXT,
    created_at        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_semantic_repairs_plan ON semantic_repairs(plan_revision_id, created_at);
CREATE INDEX IF NOT EXISTS idx_semantic_repairs_verification ON semantic_repairs(verification_id);
