package writebuffer

import "sort"

// pendingKey identifies a unique deduplicated slot in the pending map.
// OpRecordEnrichment ops bypass this — they always append.
type pendingKey struct {
	opType    OpType
	projectID string
	entityID  string
}

// pendingMap holds ops awaiting flush. It deduplicates all op types except
// OpRecordEnrichment, which always appends.
type pendingMap struct {
	dedup       map[pendingKey]WriteOp // deduplicated ops
	enrichments []WriteOp              // always-append ops
}

func newPendingMap() *pendingMap {
	return &pendingMap{
		dedup: make(map[pendingKey]WriteOp),
	}
}

// merge applies op to the pending map using per-type dedup rules:
//   - ActivationUpdate: replace with latest value (last write wins)
//   - WeightUpdate: accumulate CoActivationDelta, replace weight with latest
//   - NodeUpsert / EdgeUpsert: replace entirely (idempotent by design)
//   - ConceptUpsert: replace entirely
//   - RecordEnrichment: always append (no deduplication)
func (p *pendingMap) merge(op WriteOp) {
	if op.Type == OpRecordEnrichment {
		p.enrichments = append(p.enrichments, op)
		return
	}

	key := keyFor(op)
	existing, ok := p.dedup[key]
	if !ok {
		p.dedup[key] = op
		return
	}

	// WeightUpdate requires accumulation of CoActivationDelta.
	if op.Type == OpUpdateWeight {
		existingPayload := existing.Payload.(WeightUpdate)
		newPayload := op.Payload.(WeightUpdate)
		newPayload.CoActivationDelta += existingPayload.CoActivationDelta
		op.Payload = newPayload
	}

	// All other types: new op replaces old.
	p.dedup[key] = op
}

// len returns the total number of pending ops.
func (p *pendingMap) len() int {
	return len(p.dedup) + len(p.enrichments)
}

// drain removes and returns all pending ops, then resets the map.
// The caller owns the returned slice.
func (p *pendingMap) drain() []WriteOp {
	ops := make([]WriteOp, 0, p.len())
	for _, op := range p.dedup {
		ops = append(ops, op)
	}
	sort.SliceStable(ops, func(i, j int) bool {
		return flushOrder(ops[i].Type) < flushOrder(ops[j].Type)
	})
	ops = append(ops, p.enrichments...)

	// Reset — reuse backing arrays where possible.
	for k := range p.dedup {
		delete(p.dedup, k)
	}
	p.enrichments = p.enrichments[:0]

	return ops
}

func flushOrder(opType OpType) int {
	switch opType {
	case OpUpsertNode:
		return 10
	case OpUpsertEdge:
		return 20
	case OpUpdateActivation:
		return 30
	case OpUpdateWeight:
		return 40
	case OpUpsertConcept:
		return 50
	case OpUpsertIIR:
		return 55 // after nodes (IIR references node ids), before enrichments
	case OpUpsertSemanticPlan:
		return 56 // after IIR and nodes; recipes and descendants reference it
	case OpUpsertSemanticRecipe:
		return 57
	case OpUpsertSemanticArtifact, OpUpsertSemanticTestPlan:
		return 58
	case OpRecordSemanticVerification:
		return 59
	case OpUpsertSemanticRepair:
		return 60
	case OpRecordSemanticApproval:
		return 61
	case OpRecordEnrichment:
		return 70
	default:
		return 100
	}
}

// keyFor extracts the deduplication key for an op.
// Panics if called with OpRecordEnrichment — those are handled separately.
func keyFor(op WriteOp) pendingKey {
	var entityID string
	switch op.Type {
	case OpUpsertNode:
		entityID = op.Payload.(NodeUpsert).ID
	case OpUpsertEdge:
		entityID = op.Payload.(EdgeUpsert).ID
	case OpUpdateActivation:
		entityID = op.Payload.(ActivationUpdate).NodeID
	case OpUpdateWeight:
		entityID = op.Payload.(WeightUpdate).EdgeID
	case OpUpsertConcept:
		entityID = op.Payload.(ConceptUpsert).ID
	case OpUpsertIIR:
		entityID = op.Payload.(IIRUpsert).ID
	case OpUpsertSemanticPlan:
		entityID = op.Payload.(SemanticPlanUpsert).ID
	case OpUpsertSemanticRecipe:
		entityID = op.Payload.(SemanticRecipeUpsert).ID
	case OpUpsertSemanticArtifact:
		entityID = op.Payload.(SemanticArtifactUpsert).ID
	case OpRecordSemanticVerification:
		entityID = op.Payload.(SemanticVerificationRecord).ID
	case OpRecordSemanticApproval:
		entityID = op.Payload.(SemanticApprovalRecord).ID
	case OpUpsertSemanticTestPlan:
		entityID = op.Payload.(SemanticTestPlanUpsert).ID
	case OpUpsertSemanticRepair:
		entityID = op.Payload.(SemanticRepairUpsert).ID
	}
	return pendingKey{
		opType:    op.Type,
		projectID: op.ProjectID,
		entityID:  entityID,
	}
}
