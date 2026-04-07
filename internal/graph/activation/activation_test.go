package activation

import (
	"context"
	"testing"

	"github.com/atheory/context-engine/internal/core"
)

// testSubstrate is an in-memory SubstrateReader for testing.
type testSubstrate struct {
	nodes map[core.NodeID]*core.Node
	edges map[core.NodeID][]core.EdgeWithWeight // by source node ID
}

func newTestSubstrate() *testSubstrate {
	return &testSubstrate{
		nodes: make(map[core.NodeID]*core.Node),
		edges: make(map[core.NodeID][]core.EdgeWithWeight),
	}
}

func (s *testSubstrate) addNode(id string) core.Node {
	n := core.Node{
		ID:          core.NodeID(id),
		ProjectID:   "test",
		Type:        "symbol",
		Label:       id,
		CanonicalID: id,
	}
	s.nodes[n.ID] = &n
	return n
}

func (s *testSubstrate) addEdge(sourceID, targetID string, weight float64) core.EdgeWithWeight {
	e := core.EdgeWithWeight{
		Edge: core.Edge{
			ID:       core.EdgeID(sourceID + "->" + targetID),
			SourceID: core.NodeID(sourceID),
			TargetID: core.NodeID(targetID),
			Type:     "calls",
		},
		Weight: weight,
	}
	s.edges[core.NodeID(sourceID)] = append(s.edges[core.NodeID(sourceID)], e)
	return e
}

// SubstrateReader interface methods

func (s *testSubstrate) GetNode(_ context.Context, _ core.ProjectID, nodeID core.NodeID) (*core.Node, error) {
	return s.nodes[nodeID], nil
}

func (s *testSubstrate) GetNodeByCanonicalID(_ context.Context, _ core.ProjectID, canonicalID string) (*core.Node, error) {
	for _, n := range s.nodes {
		if n.CanonicalID == canonicalID {
			return n, nil
		}
	}
	return nil, nil
}

func (s *testSubstrate) GetNodesByNamespacePrefix(_ context.Context, _ core.ProjectID, _ string, _ int) ([]core.Node, error) {
	return nil, nil
}

func (s *testSubstrate) GetConceptNodes(_ context.Context, _ core.ProjectID, _ string) ([]core.Node, error) {
	return nil, nil
}

func (s *testSubstrate) GetNodesForFile(_ context.Context, _ core.ProjectID, _ string) ([]core.Node, error) {
	return nil, nil
}

func (s *testSubstrate) GetNodesBySuffix(_ context.Context, _ core.ProjectID, _ string, _ int) ([]core.Node, error) {
	return nil, nil
}

func (s *testSubstrate) GetTopKActivated(_ context.Context, _ core.ProjectID, _ int) ([]core.NodeWithActivation, error) {
	return nil, nil
}

func (s *testSubstrate) GetEdgesFrom(_ context.Context, _ core.ProjectID, nodeID core.NodeID) ([]core.EdgeWithWeight, error) {
	return s.edges[nodeID], nil
}

func (s *testSubstrate) GetEdgesTo(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.EdgeWithWeight, error) {
	return nil, nil
}

func (s *testSubstrate) GetEdgesBetween(_ context.Context, _ core.ProjectID, _, _ core.NodeID) ([]core.EdgeWithWeight, error) {
	return nil, nil
}

func (s *testSubstrate) GetConceptSeeds(_ context.Context, _ core.ProjectID) ([]core.ConceptSeed, error) {
	return nil, nil
}

func (s *testSubstrate) GetOrgConceptSeeds(_ context.Context) ([]core.ConceptSeed, error) {
	return nil, nil
}

// Tool-specific stubs — activation tests don't use these.

func (s *testSubstrate) GetCallers(_ context.Context, _ core.ProjectID, _ core.NodeID, _ int) ([]core.NodeWithActivation, error) {
	return nil, nil
}
func (s *testSubstrate) GetCallees(_ context.Context, _ core.ProjectID, _ core.NodeID, _ int) ([]core.NodeWithActivation, error) {
	return nil, nil
}
func (s *testSubstrate) GetReferences(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.ReferenceResult, error) {
	return nil, nil
}
func (s *testSubstrate) FindInOrgGraph(_ context.Context, _ string, _ string) ([]core.OrgMatch, error) {
	return nil, nil
}
func (s *testSubstrate) GetConceptImplementors(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.NodeWithActivation, error) {
	return nil, nil
}
func (s *testSubstrate) GetConceptSeed(_ context.Context, _ core.ProjectID, _ string) (*core.ConceptSeed, error) {
	return nil, nil
}
func (s *testSubstrate) GetFileNode(_ context.Context, _ core.ProjectID, _ string) (*core.Node, error) {
	return nil, nil
}
func (s *testSubstrate) GetFileImports(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.Node, error) {
	return nil, nil
}
func (s *testSubstrate) GetNamespaceMembers(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.Node, error) {
	return nil, nil
}
func (s *testSubstrate) GetNamespaceDependencies(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.Node, error) {
	return nil, nil
}
func (s *testSubstrate) GetNamespaceDependents(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.Node, error) {
	return nil, nil
}

// testWriter is an in-memory SubstrateWriter for testing.
type testWriter struct {
	weightUpdates []core.WeightUpdate
	decayCalled   bool
}

func (w *testWriter) UpsertNode(_ context.Context, _ core.Node) error { return nil }
func (w *testWriter) UpsertEdge(_ context.Context, _ core.Edge) error { return nil }
func (w *testWriter) UpdateActivation(_ context.Context, _ core.NodeID, _ float64) error {
	return nil
}
func (w *testWriter) UpdateEdgeWeight(_ context.Context, u core.WeightUpdate) error {
	w.weightUpdates = append(w.weightUpdates, u)
	return nil
}
func (w *testWriter) DecayEdgeWeights(_ context.Context, _ core.ProjectID, _ float64) error {
	w.decayCalled = true
	return nil
}
func (w *testWriter) ApplyEnrichment(_ context.Context, _ core.Enrichment) error { return nil }
func (w *testWriter) ResetActivation(_ context.Context, _ core.ProjectID) error  { return nil }
func (w *testWriter) Flush(_ context.Context) error                              { return nil }

// Test 1 — Spreading activation stops at threshold
func TestSpreadActivationThreshold(t *testing.T) {
	// Build a chain: A → B → C → D → E, all weights 0.5
	// Seed: A with activation 1.0
	//
	// Expected:
	//   A: 1.0  (seed)
	//   B: 1.0 * 0.6 * 0.5 = 0.30
	//   C: 0.30 * 0.6 * 0.5 = 0.09  ← below threshold, stops here
	//   D: not activated
	//   E: not activated
	ctx := context.Background()
	sub := newTestSubstrate()

	nodeA := sub.addNode("A")
	nodeB := sub.addNode("B")
	nodeC := sub.addNode("C")
	nodeD := sub.addNode("D")
	nodeE := sub.addNode("E")

	sub.addEdge("A", "B", 0.5)
	sub.addEdge("B", "C", 0.5)
	sub.addEdge("C", "D", 0.5)
	sub.addEdge("D", "E", 0.5)

	result, err := spreadActivation(ctx, sub, "test", []seedNode{
		{node: nodeA, activation: 1.0},
	})

	if err != nil {
		t.Fatalf("spreadActivation error: %v", err)
	}

	if got := result[nodeA.ID]; !almostEqual(got, 1.0) {
		t.Errorf("A activation: want 1.0, got %.4f", got)
	}
	if got := result[nodeB.ID]; !almostEqual(got, 0.30) {
		t.Errorf("B activation: want 0.30, got %.4f", got)
	}
	// C: 0.30 * 0.6 * 0.5 = 0.09 — below threshold, may or may not be in map
	// depending on when threshold check fires. The algorithm checks threshold
	// when popping, so C may be in the map with value 0.09.
	if got, ok := result[nodeD.ID]; ok {
		t.Errorf("D should not be activated, got %.4f", got)
	}
	if got, ok := result[nodeE.ID]; ok {
		t.Errorf("E should not be activated, got %.4f", got)
	}
	_ = nodeC
}

// Test 2 — High-weight edges propagate further
func TestHighWeightEdgePropagatesFurther(t *testing.T) {
	// Two paths from A:
	// Path 1: A → B (weight 0.9) → C (weight 0.9)
	// Path 2: A → D (weight 0.2) → E (weight 0.2)
	//
	// Seed: A with activation 1.0
	// Path 1: B = 1.0*0.6*0.9 = 0.54, C = 0.54*0.6*0.9 = 0.29 (above threshold)
	// Path 2: D = 1.0*0.6*0.2 = 0.12, E = 0.12*0.6*0.2 = 0.014 (below threshold)
	ctx := context.Background()
	sub := newTestSubstrate()

	nodeA := sub.addNode("A")
	nodeB := sub.addNode("B")
	nodeC := sub.addNode("C")
	nodeD := sub.addNode("D")
	nodeE := sub.addNode("E")

	sub.addEdge("A", "B", 0.9)
	sub.addEdge("B", "C", 0.9)
	sub.addEdge("A", "D", 0.2)
	sub.addEdge("D", "E", 0.2)

	result, err := spreadActivation(ctx, sub, "test", []seedNode{
		{node: nodeA, activation: 1.0},
	})

	if err != nil {
		t.Fatalf("spreadActivation error: %v", err)
	}

	if result[nodeC.ID] <= result[nodeD.ID] {
		t.Errorf("C (%.4f) should be activated more than D (%.4f)", result[nodeC.ID], result[nodeD.ID])
	}
	if _, ok := result[nodeE.ID]; ok {
		t.Errorf("E should not be activated (too low weight)")
	}
	_ = nodeB
}

// Test 3 — Hebbian strengthening
func TestHebbianStrengthening(t *testing.T) {
	ctx := context.Background()

	nodeA := core.Node{ID: "A", CanonicalID: "A"}
	nodeB := core.Node{ID: "B", CanonicalID: "B"}

	edgeAB := core.EdgeWithWeight{
		Edge: core.Edge{
			ID:       "A->B",
			SourceID: "A",
			TargetID: "B",
		},
		Weight: 0.5,
	}

	anchors := []core.Anchor{
		{Node: &nodeA, Activation: 0.8, Edges: []core.EdgeWithWeight{edgeAB}},
		{Node: &nodeB, Activation: 0.6, Edges: []core.EdgeWithWeight{edgeAB}},
	}

	writer := &testWriter{}
	err := UpdateWeights(ctx, "test", anchors, writer)
	if err != nil {
		t.Fatalf("UpdateWeights error: %v", err)
	}

	// Expected new weight: 0.5 + (0.1 * 0.8 * 0.6) = 0.5 + 0.048 = 0.548
	if len(writer.weightUpdates) == 0 {
		t.Fatal("expected weight update, got none")
	}
	got := writer.weightUpdates[0].NewWeight
	want := 0.548
	if !almostEqual(got, want) {
		t.Errorf("weight update: want %.3f, got %.4f", want, got)
	}
	if writer.weightUpdates[0].CoActivationDelta != 1 {
		t.Errorf("CoActivationDelta: want 1, got %d", writer.weightUpdates[0].CoActivationDelta)
	}
	if !writer.decayCalled {
		t.Error("DecayEdgeWeights should have been called")
	}
}

// Test 4 — speculative edge promotion
func TestSpeculativeEdgePromotion(t *testing.T) {
	// Edge starts as speculative with weight 0.38
	// Co-activation pushes it above 0.4 → promoted to associative
	ctx := context.Background()

	nodeA := core.Node{ID: "A", CanonicalID: "A"}
	nodeB := core.Node{ID: "B", CanonicalID: "B"}

	edgeAB := core.EdgeWithWeight{
		Edge: core.Edge{
			ID:          "A->B",
			SourceID:    "A",
			TargetID:    "B",
			SourceClass: "speculative",
		},
		Weight:      0.38,
		SourceClass: "speculative",
	}

	// Activation: 1.0 * 1.0 → strengthening = 0.1 * 1.0 * 1.0 = 0.1 → 0.38 + 0.1 = 0.48
	anchors := []core.Anchor{
		{Node: &nodeA, Activation: 1.0, Edges: []core.EdgeWithWeight{edgeAB}},
		{Node: &nodeB, Activation: 1.0, Edges: []core.EdgeWithWeight{edgeAB}},
	}

	writer := &testWriter{}
	err := UpdateWeights(ctx, "test", anchors, writer)
	if err != nil {
		t.Fatalf("UpdateWeights error: %v", err)
	}

	if len(writer.weightUpdates) == 0 {
		t.Fatal("expected weight update, got none")
	}
	update := writer.weightUpdates[0]
	if update.SourceClass != "associative" {
		t.Errorf("SourceClass: want associative (promoted), got %s", update.SourceClass)
	}
	if update.NewWeight < 0.4 {
		t.Errorf("weight should be above 0.4 after strengthening, got %.4f", update.NewWeight)
	}
}

// almostEqual checks float equality within 0.001.
func almostEqual(a, b float64) bool {
	diff := a - b
	if diff < 0 {
		diff = -diff
	}
	return diff < 0.001
}
