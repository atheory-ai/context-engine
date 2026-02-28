# Context Engine — Spec 10: Activation & Graph
## Implementation Spec — Spreading Activation, Hebbian Learning, Top-K Retrieval
### Version 1.0 | February 2026

---

> This spec covers the activation layer — the core of the engine's intelligence.
> Hand to Claude Code alongside spec-1-data-layer.md and spec-2-packages.md.
> Companion: Context Engine PRD v0.5 Sections 7, 8. Decisions Log v1.0 Section 2.

---

## 1. Overview

The activation layer is what separates the Context Engine from a code search
tool. It implements a spreading activation model over the substrate graph:

1. **Anchor resolution** — map IR anchor refs to actual substrate nodes
2. **Spreading activation** — propagate activation outward from anchor nodes,
   decaying by distance and weighted by edge weights
3. **Top-K retrieval** — return the K most activated nodes for tool fan-out
4. **Hebbian weight updates** — strengthen edges between nodes that co-activate
   in the same cognitive loop (learning from usage)

The model is grounded in cognitive science. Spreading activation is how
associative memory works — a concept activates related concepts, which activate
their neighbors, with strength decaying by distance and connection weight.
Over time, frequently co-activated nodes develop stronger connections and
surface together more readily.

---

## 2. Package Structure

```
internal/graph/
  activation/
    activation.go     — Node struct, Run() — the activation pass
    propagate.go      — spreading activation algorithm
    resolve.go        — anchor ref → substrate node resolution
    topk.go           — top-K node selection
    hebbian.go        — weight update after co-activation
  substrate/
    reader.go         — SubstrateReader implementation
    writer.go         — SubstrateWriter implementation
    readwriter.go     — combined ReadWriter
  ontology/
    ontology.go       — concept seed management, synonym resolution
```

---

## 3. Activation Values and Constants

```go
// internal/core/constants.go (additions)

const (
    // Initial activation values by anchor confidence
    ActivationHighConfidence   = 1.0
    ActivationMediumConfidence = 0.7
    ActivationLowConfidence    = 0.4

    // Propagation stops when activation falls below this threshold
    ActivationThreshold = 0.1

    // Safety cap on propagation depth regardless of activation level
    MaxPropagationDepth = 6

    // Activation decay per hop — multiplied by edge weight
    // activation_at_neighbor = source_activation * decay * edge_weight
    ActivationDecay = 0.6

    // Default edge weight for new structural edges
    DefaultEdgeWeight = 0.5

    // Edge weight range
    MinEdgeWeight = 0.01
    MaxEdgeWeight = 1.0

    // Hebbian learning rate — how much weight increases per co-activation
    HebbianLearningRate = 0.1

    // Hebbian decay rate — how much weight decreases when not co-activated
    // Applied to all edges at the end of each cognitive loop
    HebbianDecayRate = 0.01

    // Default K for top-K retrieval (overridden by IR.KLimit)
    DefaultKLimit = 30

    // Context window safety margin (also used in budget)
    ContextWindowSafetyMargin = 0.85
)
```

---

## 4. The Activation Node

The activation node is called once per cognitive loop iteration. It takes
the current IR and returns the top-K activated nodes as Anchors.

```go
// internal/graph/activation/activation.go

package activation

// Node is the activation pass in the cognitive loop DAG.
type Node struct {
    substrate core.SubstrateReader
}

func NewNode(substrate core.SubstrateReader) *Node {
    return &Node{substrate: substrate}
}

// Run executes one activation pass for the current loop iteration.
// Returns the top-K activated nodes as Anchors, ready for tool fan-out.
func (n *Node) Run(rc *runner.RunContext) ([]core.Anchor, error) {
    ir := rc.IR
    kLimit := ir.KLimit
    if kLimit == 0 {
        kLimit = core.DefaultKLimit
    }

    // ── 1. Resolve anchor refs to substrate nodes ──────────────────────────
    seedNodes, err := resolveAnchors(rc.Ctx, n.substrate, rc.ProjectID, ir.Anchors)
    if err != nil {
        return nil, fmt.Errorf("resolve anchors: %w", err)
    }

    if len(seedNodes) == 0 {
        rc.Ch.Emit(core.Emission{
            RunID:   rc.RunID,
            TurnID:  rc.TurnID,
            Channel: core.ChanWarning,
            Content: fmt.Sprintf("no seed nodes resolved from %d anchor refs", len(ir.Anchors)),
        })
        // Return empty — the loop continues, Reviewer will note the gap
        return nil, nil
    }

    rc.Ch.Emit(core.Emission{
        RunID:   rc.RunID,
        TurnID:  rc.TurnID,
        Channel: core.ChanThinking,
        Content: fmt.Sprintf("resolved %d/%d anchors to substrate nodes",
            len(seedNodes), len(ir.Anchors)),
    })

    // ── 2. Run spreading activation ────────────────────────────────────────
    activationMap, err := spreadActivation(rc.Ctx, n.substrate, rc.ProjectID, seedNodes)
    if err != nil {
        return nil, fmt.Errorf("spread activation: %w", err)
    }

    rc.Ch.Emit(core.Emission{
        RunID:   rc.RunID,
        TurnID:  rc.TurnID,
        Channel: core.ChanThinking,
        Content: fmt.Sprintf("activation spread to %d nodes", len(activationMap)),
    })

    // ── 3. Persist activation values to write buffer ───────────────────────
    // These are written to node_activation table asynchronously.
    // The write buffer deduplicates concurrent updates.
    for nodeID, activation := range activationMap {
        rc.Substrate.UpdateActivation(rc.Ctx, nodeID, activation)
    }

    // ── 4. Select top-K nodes ──────────────────────────────────────────────
    topK := selectTopK(activationMap, kLimit)

    // ── 5. Enrich top-K nodes with edges for tool context ─────────────────
    anchors, err := enrichAnchors(rc.Ctx, n.substrate, rc.ProjectID, topK)
    if err != nil {
        return nil, fmt.Errorf("enrich anchors: %w", err)
    }

    return anchors, nil
}
```

---

## 5. Anchor Resolution

Anchor refs from the IR are symbolic — they may or may not exist in the
substrate. Resolution finds matching nodes with graceful fallback.

```go
// internal/graph/activation/resolve.go

package activation

// resolveAnchors maps IR anchor refs to substrate nodes.
// Returns seed nodes with their initial activation values.
// Unresolvable anchors are logged and skipped (non-fatal).
func resolveAnchors(
    ctx        context.Context,
    substrate  core.SubstrateReader,
    projectID  core.ProjectID,
    anchorRefs []core.AnchorRef,
) ([]seedNode, error) {

    var seeds []seedNode

    for _, ref := range anchorRefs {
        initialActivation := confidenceToActivation(ref.Confidence)

        nodes, err := findNodesForRef(ctx, substrate, projectID, ref)
        if err != nil {
            return nil, err
        }

        if len(nodes) == 0 {
            // Try fuzzy resolution — prefix match on canonical ID
            nodes, err = fuzzyResolveRef(ctx, substrate, projectID, ref)
            if err != nil {
                return nil, err
            }
        }

        for _, node := range nodes {
            seeds = append(seeds, seedNode{
                node:       node,
                activation: initialActivation,
            })
        }
    }

    return seeds, nil
}

type seedNode struct {
    node       core.Node
    activation float64
}

func confidenceToActivation(confidence string) float64 {
    switch confidence {
    case "high":
        return core.ActivationHighConfidence
    case "medium":
        return core.ActivationMediumConfidence
    case "low":
        return core.ActivationLowConfidence
    default:
        return core.ActivationMediumConfidence
    }
}

// findNodesForRef looks up nodes by type and canonical ID.
func findNodesForRef(
    ctx       context.Context,
    substrate core.SubstrateReader,
    projectID core.ProjectID,
    ref       core.AnchorRef,
) ([]core.Node, error) {

    switch ref.Type {
    case "symbol":
        // Exact match on canonical ID
        node, err := substrate.GetNodeByCanonicalID(ctx, projectID, ref.ID)
        if err != nil || node == nil {
            return nil, err
        }
        return []core.Node{*node}, nil

    case "namespace":
        // All nodes whose canonical ID starts with this namespace
        return substrate.GetNodesByNamespacePrefix(ctx, projectID, ref.ID, 20)

    case "concept":
        // Concept nodes plus synonym expansion
        return substrate.GetConceptNodes(ctx, projectID, ref.ID)

    case "file":
        // File node plus all nodes extracted from this file
        return substrate.GetNodesForFile(ctx, projectID, ref.ID)

    default:
        return nil, nil
    }
}

// fuzzyResolveRef attempts prefix and suffix matching when exact match fails.
// Example: "ProcessPayment" matches "internal/billing:ProcessPayment"
func fuzzyResolveRef(
    ctx       context.Context,
    substrate core.SubstrateReader,
    projectID core.ProjectID,
    ref       core.AnchorRef,
) ([]core.Node, error) {

    if ref.Type != "symbol" {
        return nil, nil // fuzzy resolution only for symbols
    }

    // Try suffix match — "ProcessPayment" matches any canonicalID ending in ":ProcessPayment"
    nodes, err := substrate.GetNodesBySuffix(ctx, projectID, ref.ID, 5)
    if err != nil {
        return nil, err
    }

    // Reduce activation for fuzzy matches — less confident than exact
    // Caller handles the reduction via confidence already being set
    return nodes, nil
}
```

---

## 6. Spreading Activation Algorithm

The core algorithm. Propagates activation outward from seed nodes,
decaying by distance and weighted by edge weights.

```go
// internal/graph/activation/propagate.go

package activation

// activationEntry tracks a node's current activation and propagation state.
type activationEntry struct {
    nodeID     core.NodeID
    activation float64
    depth      int
}

// spreadActivation runs the spreading activation algorithm from seed nodes.
// Returns a map of nodeID → final activation value for all activated nodes.
//
// Algorithm:
//   1. Initialize priority queue with seed nodes at their initial activation
//   2. Pop highest-activation node from queue
//   3. For each outgoing edge from that node:
//      a. Compute neighbor activation = current * decay * edge_weight
//      b. If neighbor_activation > threshold AND depth < max_depth:
//         - If neighbor not yet visited OR new activation > existing:
//           push to queue with new activation
//   4. Record final activation for each visited node
//   5. Repeat until queue empty
//
// This is a variant of Dijkstra's algorithm adapted for activation spreading
// rather than shortest path. Higher activation = higher priority.
func spreadActivation(
    ctx       context.Context,
    substrate core.SubstrateReader,
    projectID core.ProjectID,
    seeds     []seedNode,
) (map[core.NodeID]float64, error) {

    // activationMap tracks the best (highest) activation seen for each node
    activationMap := make(map[core.NodeID]float64)

    // Priority queue — highest activation first
    pq := newActivationQueue()

    // Initialize with seed nodes
    for _, seed := range seeds {
        activationMap[seed.node.ID] = seed.activation
        pq.Push(activationEntry{
            nodeID:     seed.node.ID,
            activation: seed.activation,
            depth:      0,
        })
    }

    // Propagate
    for pq.Len() > 0 {
        // Check context cancellation
        select {
        case <-ctx.Done():
            return activationMap, ctx.Err()
        default:
        }

        current := pq.Pop()

        // Skip if we've already processed this node at higher activation
        // (stale entry in the priority queue)
        if existing, ok := activationMap[current.nodeID]; ok {
            if existing > current.activation {
                continue
            }
        }

        // Stop propagating if below threshold or at max depth
        if current.activation < core.ActivationThreshold {
            continue
        }
        if current.depth >= core.MaxPropagationDepth {
            continue
        }

        // Fetch outgoing edges for this node
        edges, err := substrate.GetEdgesFrom(ctx, projectID, current.nodeID)
        if err != nil {
            return nil, fmt.Errorf("get edges from %s: %w", current.nodeID, err)
        }

        for _, edge := range edges {
            neighborActivation := current.activation *
                core.ActivationDecay *
                edge.Weight

            if neighborActivation < core.ActivationThreshold {
                continue
            }

            // Only update if this activation is higher than what we've seen
            if existing, ok := activationMap[edge.TargetID]; ok {
                if neighborActivation <= existing {
                    continue
                }
            }

            activationMap[edge.TargetID] = neighborActivation
            pq.Push(activationEntry{
                nodeID:     edge.TargetID,
                activation: neighborActivation,
                depth:      current.depth + 1,
            })
        }
    }

    return activationMap, nil
}

// activationQueue is a max-heap priority queue ordered by activation value.
type activationQueue struct {
    items []activationEntry
}

func newActivationQueue() *activationQueue {
    return &activationQueue{}
}

func (q *activationQueue) Len() int { return len(q.items) }

func (q *activationQueue) Push(entry activationEntry) {
    q.items = append(q.items, entry)
    q.siftUp(len(q.items) - 1)
}

func (q *activationQueue) Pop() activationEntry {
    top := q.items[0]
    last := len(q.items) - 1
    q.items[0] = q.items[last]
    q.items = q.items[:last]
    if len(q.items) > 0 {
        q.siftDown(0)
    }
    return top
}

func (q *activationQueue) siftUp(i int) {
    for i > 0 {
        parent := (i - 1) / 2
        if q.items[parent].activation >= q.items[i].activation {
            break
        }
        q.items[parent], q.items[i] = q.items[i], q.items[parent]
        i = parent
    }
}

func (q *activationQueue) siftDown(i int) {
    n := len(q.items)
    for {
        largest := i
        left, right := 2*i+1, 2*i+2
        if left < n && q.items[left].activation > q.items[largest].activation {
            largest = left
        }
        if right < n && q.items[right].activation > q.items[largest].activation {
            largest = right
        }
        if largest == i {
            break
        }
        q.items[i], q.items[largest] = q.items[largest], q.items[i]
        i = largest
    }
}
```

---

## 7. Top-K Selection

```go
// internal/graph/activation/topk.go

package activation

// activatedNode pairs a node ID with its activation value.
type activatedNode struct {
    nodeID     core.NodeID
    activation float64
}

// selectTopK returns the K nodes with highest activation values.
// Returns fewer than K if fewer nodes were activated.
func selectTopK(activationMap map[core.NodeID]float64, k int) []activatedNode {
    // Collect all activated nodes
    nodes := make([]activatedNode, 0, len(activationMap))
    for nodeID, activation := range activationMap {
        nodes = append(nodes, activatedNode{nodeID, activation})
    }

    // Sort by activation descending
    sort.Slice(nodes, func(i, j int) bool {
        return nodes[i].activation > nodes[j].activation
    })

    // Return top K
    if len(nodes) > k {
        nodes = nodes[:k]
    }
    return nodes
}

// enrichAnchors fetches full node data and connected edges for the top-K nodes.
// Returns Anchors ready for tool execution.
func enrichAnchors(
    ctx       context.Context,
    substrate core.SubstrateReader,
    projectID core.ProjectID,
    topK      []activatedNode,
) ([]core.Anchor, error) {

    anchors := make([]core.Anchor, 0, len(topK))

    for _, activated := range topK {
        node, err := substrate.GetNode(ctx, projectID, activated.nodeID)
        if err != nil || node == nil {
            continue // node may have been deleted — skip
        }

        // Fetch edges for context
        edges, err := substrate.GetEdgesFrom(ctx, projectID, activated.nodeID)
        if err != nil {
            return nil, fmt.Errorf("get edges for anchor %s: %w", activated.nodeID, err)
        }

        // Also fetch incoming edges — tools need both directions
        inEdges, err := substrate.GetEdgesTo(ctx, projectID, activated.nodeID)
        if err != nil {
            return nil, fmt.Errorf("get in-edges for anchor %s: %w", activated.nodeID, err)
        }

        allEdges := append(edges, inEdges...)

        anchors = append(anchors, core.Anchor{
            Ref: core.AnchorRef{
                Type:       node.Type,
                ID:         node.CanonicalID,
                Confidence: activationToConfidence(activated.activation),
            },
            Node:       node,
            Edges:      allEdges,
            Activation: activated.activation,
        })
    }

    return anchors, nil
}

func activationToConfidence(activation float64) string {
    switch {
    case activation >= core.ActivationHighConfidence * 0.8:
        return "high"
    case activation >= core.ActivationMediumConfidence * 0.8:
        return "medium"
    default:
        return "low"
    }
}
```

---

## 8. Hebbian Weight Updates

After each cognitive loop iteration, edges between co-activated nodes
are strengthened. Edges that were not involved in the activation are
slightly weakened. Over time, the graph learns which connections are
most useful for the kinds of queries this project receives.

```go
// internal/graph/activation/hebbian.go

package activation

// UpdateWeights applies Hebbian learning after a cognitive loop iteration.
// Called by the runner after the Reviewer pass, before the next iteration.
//
// Hebbian rule: "neurons that fire together, wire together"
// Edges between co-activated nodes get stronger.
// Edges not involved in this activation get slightly weaker (prevents runaway growth).
func UpdateWeights(
    ctx          context.Context,
    projectID    core.ProjectID,
    anchors      []core.Anchor,
    substrate    core.SubstrateWriter,
) error {

    if len(anchors) == 0 {
        return nil
    }

    // Build set of activated node IDs for fast lookup
    activatedIDs := make(map[core.NodeID]float64)
    for _, anchor := range anchors {
        if anchor.Node != nil {
            activatedIDs[anchor.Node.ID] = anchor.Activation
        }
    }

    // Collect all edges between activated nodes
    // These are the edges to strengthen
    coActivatedEdges := make(map[core.EdgeID]coActivation)

    for _, anchor := range anchors {
        if anchor.Node == nil {
            continue
        }
        for _, edge := range anchor.Edges {
            // Check if both source and target were activated
            sourceActivation, sourceActive := activatedIDs[edge.SourceID]
            targetActivation, targetActive := activatedIDs[edge.TargetID]

            if sourceActive && targetActive {
                // Weight increase proportional to the product of activations
                // (classic Hebbian rule)
                strengthening := core.HebbianLearningRate *
                    sourceActivation * targetActivation

                coActivatedEdges[edge.ID] = coActivation{
                    edge:        edge,
                    strengthening: strengthening,
                }
            }
        }
    }

    // Apply weight updates via write buffer
    for _, ca := range coActivatedEdges {
        newWeight := clamp(
            ca.edge.Weight + ca.strengthening,
            core.MinEdgeWeight,
            core.MaxEdgeWeight,
        )

        substrate.UpdateEdgeWeight(ctx, core.WeightUpdate{
            EdgeID:             ca.edge.ID,
            ProjectID:          projectID,
            NewWeight:          newWeight,
            CoActivationDelta:  1,
            SourceClass:        updateSourceClass(ca.edge.SourceClass, newWeight),
        })
    }

    // Apply decay to all other edges for this project
    // This is done asynchronously via write buffer — fire and forget
    substrate.DecayEdgeWeights(ctx, projectID, core.HebbianDecayRate)

    return nil
}

type coActivation struct {
    edge        core.Edge
    strengthening float64
}

// updateSourceClass promotes edges that reach high weight thresholds.
// speculative → associative when weight reaches 0.4
// associative → structural requires human confirmation (not automatic)
func updateSourceClass(current string, newWeight float64) string {
    if current == "speculative" && newWeight >= 0.4 {
        return "associative"
    }
    return current
}

func clamp(v, min, max float64) float64 {
    if v < min { return min }
    if v > max { return max }
    return v
}
```

### Edge Decay Implementation

Edge decay applies a small weight reduction to all edges that were NOT
co-activated in a loop. This prevents the graph from accumulating stale
high-weight edges from old usage patterns.

```go
// internal/storage/queries/edges.go (addition)

// DecayEdgeWeights reduces all edge weights for a project by decayRate.
// Weights that fall below MinEdgeWeight are not modified (floor).
// Runs as a single UPDATE statement — efficient even for large graphs.
func (q *EdgeQueries) DecayEdgeWeights(
    ctx       context.Context,
    projectID core.ProjectID,
    decayRate float64,
) error {
    db := q.registry.ProjectDB(string(projectID))

    _, err := db.ExecContext(ctx, `
        UPDATE edge_weight
        SET weight = MAX(?, weight * (1.0 - ?)),
            updated_at = ?
        WHERE edge_id IN (
            SELECT ew.edge_id FROM edge_weight ew
            JOIN edges e ON e.id = ew.edge_id
            WHERE e.project_id = ?
        )
    `, core.MinEdgeWeight, decayRate, time.Now().UnixMilli(), string(projectID))

    return err
}
```

---

## 9. SubstrateReader Interface — Full Definition

The activation layer relies heavily on the substrate reader. This is the
complete interface definition (amends the partial definition in Spec 2).

```go
// internal/core/interfaces.go (SubstrateReader — complete definition)

type SubstrateReader interface {
    // Node retrieval
    GetNode(ctx context.Context, projectID ProjectID, nodeID NodeID) (*Node, error)
    GetNodeByCanonicalID(ctx context.Context, projectID ProjectID, canonicalID string) (*Node, error)
    GetNodesByNamespacePrefix(ctx context.Context, projectID ProjectID, prefix string, limit int) ([]Node, error)
    GetConceptNodes(ctx context.Context, projectID ProjectID, term string) ([]Node, error)
    GetNodesForFile(ctx context.Context, projectID ProjectID, filePath string) ([]Node, error)
    GetNodesBySuffix(ctx context.Context, projectID ProjectID, suffix string, limit int) ([]Node, error)

    // Top-K activation query (hot path — must use index)
    GetTopKActivated(ctx context.Context, projectID ProjectID, k int) ([]NodeWithActivation, error)

    // Edge retrieval
    GetEdgesFrom(ctx context.Context, projectID ProjectID, nodeID NodeID) ([]EdgeWithWeight, error)
    GetEdgesTo(ctx context.Context, projectID ProjectID, nodeID NodeID) ([]EdgeWithWeight, error)
    GetEdgesBetween(ctx context.Context, projectID ProjectID, sourceID, targetID NodeID) ([]EdgeWithWeight, error)

    // Concept seeds
    GetConceptSeeds(ctx context.Context, projectID ProjectID) ([]ConceptSeed, error)
    GetOrgConceptSeeds(ctx context.Context) ([]ConceptSeed, error)
}

// NodeWithActivation pairs a node with its current activation value.
type NodeWithActivation struct {
    Node
    Activation float64
}

// EdgeWithWeight pairs an edge with its current weight.
type EdgeWithWeight struct {
    Edge
    Weight           float64
    SourceClass      string
    CoActivationCount int
}
```

---

## 10. SubstrateWriter Interface — Full Definition

```go
// internal/core/interfaces.go (SubstrateWriter — complete definition)

type SubstrateWriter interface {
    // Node and edge upserts (go through write buffer)
    UpsertNode(ctx context.Context, node Node) error
    UpsertEdge(ctx context.Context, edge Edge) error

    // Activation updates (high frequency — write buffer deduplicates)
    UpdateActivation(ctx context.Context, nodeID NodeID, activation float64) error

    // Edge weight updates (from Hebbian learning)
    UpdateEdgeWeight(ctx context.Context, update WeightUpdate) error

    // Decay all edges for a project (single SQL UPDATE)
    DecayEdgeWeights(ctx context.Context, projectID ProjectID, decayRate float64) error

    // Enrichment proposals from Reviewer (Reviewer-approved substrate changes)
    ApplyEnrichment(ctx context.Context, enrichment Enrichment) error

    // Flush — blocks until write buffer is empty
    Flush(ctx context.Context) error
}

// WeightUpdate carries the data for an edge weight update operation.
type WeightUpdate struct {
    EdgeID            EdgeID
    ProjectID         ProjectID
    NewWeight         float64
    CoActivationDelta int     // increment to co_activation_count
    SourceClass       string  // updated source class if promotion occurred
}
```

---

## 11. Activation Queries — Hot Path SQL

These queries must use indexes. Validate with EXPLAIN QUERY PLAN before shipping.

```sql
-- GetTopKActivated — used by activation node to fetch current state
-- Index: idx_node_activation_activation (on node_activation.activation DESC)
SELECT
    n.id, n.type, n.label, n.canonical_id, n.source_class,
    n.plugin_id, n.properties,
    na.activation, na.peak_activation
FROM nodes n
JOIN node_activation na ON na.node_id = n.id
WHERE n.project_id = ?
ORDER BY na.activation DESC
LIMIT ?;

-- GetEdgesFrom — used during activation propagation (innermost loop)
-- Index: idx_edges_source (on edges.source_id)
SELECT
    e.id, e.source_id, e.target_id, e.type, e.source_class,
    e.plugin_id, e.properties,
    ew.weight, ew.source_class as weight_source_class,
    ew.co_activation_count
FROM edges e
JOIN edge_weight ew ON ew.edge_id = e.id
WHERE e.project_id = ? AND e.source_id = ?
ORDER BY ew.weight DESC;

-- GetNodeByCanonicalID — used during anchor resolution
-- Index: idx_nodes_canonical (on nodes.canonical_id, project_id)
SELECT
    id, type, label, canonical_id, source_class,
    plugin_id, properties, created_at, updated_at
FROM nodes
WHERE project_id = ? AND canonical_id = ?
LIMIT 1;

-- GetNodesByNamespacePrefix — used for namespace anchor refs
-- Index: idx_nodes_canonical (prefix scan)
SELECT
    id, type, label, canonical_id, source_class,
    plugin_id, properties, created_at, updated_at
FROM nodes
WHERE project_id = ? AND canonical_id LIKE ? || '%'
ORDER BY canonical_id
LIMIT ?;

-- UpdateActivation — write buffer deduplicates, this runs once per flush
INSERT INTO node_activation (node_id, activation, peak_activation, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(node_id) DO UPDATE SET
    activation = excluded.activation,
    peak_activation = MAX(peak_activation, excluded.activation),
    updated_at = excluded.updated_at;
```

---

## 12. Concept Seed Expansion

Concept anchors expand laterally via synonym resolution before activation
propagates. A query for "billing-event" also activates "invoice", "charge",
"payment" if they are registered as related concepts.

```go
// internal/graph/ontology/ontology.go

package ontology

// Ontology manages concept seeds and synonym resolution.
type Ontology struct {
    substrate core.SubstrateReader
}

// ExpandConcept returns a concept node plus all related concept nodes.
// Used during anchor resolution for concept-type anchor refs.
func (o *Ontology) ExpandConcept(
    ctx       context.Context,
    projectID core.ProjectID,
    term      string,
) ([]core.Node, error) {

    // Get the primary concept node
    primaryNodes, err := o.substrate.GetConceptNodes(ctx, projectID, term)
    if err != nil {
        return nil, err
    }

    // Get concept seeds for related terms
    seeds, err := o.substrate.GetConceptSeeds(ctx, projectID)
    if err != nil {
        return nil, err
    }

    // Find related terms
    relatedTerms := findRelatedTerms(term, seeds)

    // Fetch nodes for related terms (lower initial activation)
    var allNodes []core.Node
    allNodes = append(allNodes, primaryNodes...)

    for _, related := range relatedTerms {
        relatedNodes, err := o.substrate.GetConceptNodes(ctx, projectID, related)
        if err != nil {
            continue // non-fatal
        }
        allNodes = append(allNodes, relatedNodes...)
    }

    return allNodes, nil
}

func findRelatedTerms(term string, seeds []core.ConceptSeed) []string {
    for _, seed := range seeds {
        if seed.Term == term {
            related := make([]string, 0, len(seed.Related)+len(seed.Synonyms))
            related = append(related, seed.Related...)
            related = append(related, seed.Synonyms...)
            return related
        }
    }
    return nil
}
```

---

## 13. Activation Reset Between Queries

Activation values from one query must not bleed into the next. Each new
query starts with a clean activation state.

```go
// internal/graph/activation/activation.go (addition)

// ResetActivation zeroes all activation values for a project.
// Called by the runner at the start of each new query.
// Runs as a single UPDATE — fast even for large graphs.
func ResetActivation(
    ctx       context.Context,
    projectID core.ProjectID,
    substrate core.SubstrateWriter,
) error {
    return substrate.ResetActivation(ctx, projectID)
}
```

```sql
-- ResetActivation SQL
UPDATE node_activation
SET activation = 0.0,
    updated_at = ?
WHERE node_id IN (
    SELECT na.node_id FROM node_activation na
    JOIN nodes n ON n.id = na.node_id
    WHERE n.project_id = ?
    AND na.activation > 0.0
);
```

Note: `peak_activation` is NOT reset. It accumulates across queries and
represents the historical maximum activation — useful for identifying the
most consistently relevant nodes for a project over time.

---

## 14. Runner Amendment — Hebbian Updates

The runner's `runLoop()` (from Spec 3) is amended to call `UpdateWeights`
after the Reviewer pass and before checking convergence:

```go
// internal/runner/loop.go (amended — add after Reviewer pass)

// Apply Hebbian weight updates for this iteration's co-activations
if err := activation.UpdateWeights(
    rc.Ctx,
    rc.ProjectID,
    rc.ReadAnchors(),
    d.engine.substrate,
); err != nil {
    // Non-fatal — log warning, continue
    rc.Ch.Emit(core.Emission{
        Channel: core.ChanWarning,
        Content: fmt.Sprintf("hebbian update: %v", err),
    })
}

// Reset activation before next iteration
if !review.Converged {
    if err := activation.ResetActivation(rc.Ctx, rc.ProjectID, d.engine.substrate); err != nil {
        return fmt.Errorf("reset activation: %w", err)
    }
}
```

---

## 15. Test Cases

### Test 1 — Spreading activation stops at threshold

```go
func TestSpreadActivationThreshold(t *testing.T) {
    // Build a chain: A → B → C → D → E
    // Edge weights: all 0.5
    // Seed: A with activation 1.0
    //
    // Expected activations:
    // A: 1.0 (seed)
    // B: 1.0 * 0.6 * 0.5 = 0.30
    // C: 0.30 * 0.6 * 0.5 = 0.09  ← below threshold (0.1), stops here
    // D: not activated
    // E: not activated

    substrate := newTestSubstrate(t)
    // ... setup nodes and edges ...

    result, err := spreadActivation(ctx, substrate, projectID, []seedNode{
        {node: nodeA, activation: 1.0},
    })

    assert.NoError(t, err)
    assert.InDelta(t, 1.0,  result[nodeA.ID], 0.001)
    assert.InDelta(t, 0.30, result[nodeB.ID], 0.001)
    assert.InDelta(t, 0.09, result[nodeC.ID], 0.001)
    assert.NotContains(t, result, nodeD.ID)
    assert.NotContains(t, result, nodeE.ID)
}
```

### Test 2 — High-weight edges propagate further

```go
func TestHighWeightEdgePropagatesFurther(t *testing.T) {
    // Two paths from A:
    // Path 1: A → B (weight 0.9) → C (weight 0.9)
    // Path 2: A → D (weight 0.2) → E (weight 0.2)
    //
    // Seed: A with activation 1.0
    // Path 1: B = 0.54, C = 0.29 (both above threshold)
    // Path 2: D = 0.12, E = 0.014 (E below threshold — stops)

    // ... test implementation ...

    assert.Greater(t, result[nodeC.ID], result[nodeD.ID])
    assert.NotContains(t, result, nodeE.ID)
}
```

### Test 3 — Hebbian strengthening

```go
func TestHebbianStrengthening(t *testing.T) {
    substrate := newTestSubstrate(t)
    // Edge A→B with initial weight 0.5

    anchors := []core.Anchor{
        {Node: &nodeA, Activation: 0.8, Edges: []core.EdgeWithWeight{edgeAB}},
        {Node: &nodeB, Activation: 0.6, Edges: []core.EdgeWithWeight{edgeAB}},
    }

    err := UpdateWeights(ctx, projectID, anchors, substrate)
    assert.NoError(t, err)

    // Expected new weight: 0.5 + (0.1 * 0.8 * 0.6) = 0.5 + 0.048 = 0.548
    updatedEdge := substrate.GetEdge(t, edgeAB.ID)
    assert.InDelta(t, 0.548, updatedEdge.Weight, 0.001)
    assert.Equal(t, 1, updatedEdge.CoActivationCount)
}
```

### Test 4 — speculative edge promotion

```go
func TestSpeculativeEdgePromotion(t *testing.T) {
    // Edge starts as speculative with weight 0.38
    // One co-activation pushes it above 0.4 → promoted to associative

    initialEdge := core.EdgeWithWeight{
        Edge:        core.Edge{SourceClass: "speculative"},
        Weight:      0.38,
    }

    // activation product: 1.0 * 0.9 = 0.9
    // strengthening: 0.1 * 0.9 = 0.09
    // new weight: 0.38 + 0.09 = 0.47 > 0.4 → promote

    newClass := updateSourceClass("speculative", 0.47)
    assert.Equal(t, "associative", newClass)
}
```

---

## 16. Package Layout Summary

```
internal/graph/
  activation/
    activation.go     — Node struct, Run(), ResetActivation()
    propagate.go      — spreadActivation(), activationQueue
    resolve.go        — resolveAnchors(), findNodesForRef(), fuzzyResolveRef()
    topk.go           — selectTopK(), enrichAnchors()
    hebbian.go        — UpdateWeights(), updateSourceClass(), clamp()
    activation_test.go — all test cases
  substrate/
    reader.go         — SubstrateReader SQL implementation
    writer.go         — SubstrateWriter (delegates to write buffer)
    readwriter.go     — combined ReadWriter
  ontology/
    ontology.go       — ExpandConcept(), findRelatedTerms()
```

---

## 17. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| Propagation model | Spreading activation (not direct retrieval) |
| Propagation bound | Threshold (0.1) AND max depth (6) — both enforced |
| Priority queue | Max-heap ordered by activation value |
| Decay per hop | 0.6 × edge_weight |
| Initial activation | 1.0 / 0.7 / 0.4 by confidence (high/medium/low) |
| Top-K selection | Sort by activation descending, take first K |
| Anchor enrichment | Both outgoing and incoming edges fetched |
| Hebbian rule | weight += learning_rate × source_activation × target_activation |
| Learning rate | 0.1 |
| Decay rate | 0.01 per loop iteration (applied to all non-co-activated edges) |
| Source class promotion | speculative → associative at weight ≥ 0.4 (automatic) |
| Source class promotion | associative → structural requires human confirmation |
| Activation reset | Between queries (not between loop iterations) |
| Peak activation | Never reset — accumulates as historical record |
| Concept expansion | Related terms and synonyms activated at same confidence level |
| Fuzzy anchor resolution | Suffix match for symbol refs — non-fatal if no match |

---

*Spec 10: Activation & Graph — v1.0 — February 2026*
*Next: Spec 11 — Built-in Tools*
*Companion: Context Engine PRD v0.5 Sections 7, 8 | Decisions Log v1.0 Section 2*
