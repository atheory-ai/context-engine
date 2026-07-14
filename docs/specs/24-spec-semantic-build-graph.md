# Context Engine — Spec 24: Semantic Build Graph

## Implementation spec — durable plans, artifacts, provenance, and semantic diffs

Status: implemented (foundation, 2026-07-14). Depends on Specs 19, 22, 23, 28,
30, and 31, plus Spec 1 (data layer).

Implementation: the graph migration creates immutable, foreign-keyed plan,
recipe, artifact, verification, approval, test-plan, and repair records. All
semantic writes are typed write-buffer operations; query APIs provide history,
lineage, unresolved questions, and plan-ID semantic diffs. The initial
retention policy marks artifacts stale when incremental pruning removes their
sole source unit, retaining hash/provenance records indefinitely until a future
policy-driven compactor is introduced.

## Goal

Persist semantic-development artifacts as first-class, queryable units linked to
the existing substrate. A semantic unit should carry its plan revisions,
generation artifacts, observed semantics, verification results, approvals, and
source/graph evidence.

This makes targeted repair, review, impact analysis, and future coordination
possible without overloading `nodes.properties` or `enrichments`.

## Storage design

Add table-per-concern migrations to the graph database:

- `semantic_plans`: immutable canonical plan payloads, revision, lifecycle,
  parent plan ID, project/unit IDs, schema version, timestamps.
- `semantic_recipes`: immutable canonical recipe payloads, renderer profile,
  source plan revision, schema version, and timestamps.
- `semantic_artifacts`: renderer input/output metadata, content hash, target
  language/path, and the exact recipe and plan revisions used. Source content is
  stored only when project policy permits; otherwise retain a hash and source
  reference.
- `semantic_verifications`: report payload, verdict, verifier version, plan and
  observed-IIR references, timestamps.
- `semantic_approvals`: explicit user decisions, rationale, scope, and audit
  identity.

Use deterministic IDs where the identity is stable; revisions receive immutable
IDs derived from the plan content and parent. Add indexes by project, unit node,
parent revision, artifact, and verdict.

`iir` remains the table for per-function declared/observed IIR. Plan and recipe
tables reference it where needed; they do not duplicate it as an unstructured
blob.

## Write and read boundaries

Add typed records to `core` only where they are broadly substrate-facing; keep
semantic model interpretation in `internal/semantic`. Extend
`core.SubstrateWriter` and the graph writer with typed enqueue methods. Every
mutation goes through a new write-buffer operation, ordered after referenced
nodes/IIR and before enrichments. Never issue direct graph-DB writes.

Read-scoped sessions can construct plans in memory and query stored artifacts,
but cannot create plan revisions, approvals, verification runs, or source
artifacts. Writes carry run/turn context where available for auditability.

## Query surfaces

Provide read APIs for a unit's latest plan, plan/recipe revision history,
artifacts, verification timeline, repair/test-plan lineage, semantic diff, and
unresolved questions. Add thin CLI, MCP, and REST adapters only after the
storage/query layer has contract tests. Semantic diff is plan-ID based:
bindings, claims, obligations, decisions, recipe steps, and verification
verdicts—not source-text diff.

## Acceptance criteria

- Migrations are forward/backward tested and table constraints reject invalid
  foreign references or schema versions.
- Replaying the same plan/recipe/artifact write is idempotent.
- Incremental reindex pruning removes or marks stale artifacts linked solely to
  removed source units according to a documented retention policy.
- A read-scoped test proves no execution or graph record is created.
- Storage/query and write-buffer tests cover plan history and semantic diff.
