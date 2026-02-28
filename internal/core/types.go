package core

import (
	"crypto/sha256"
	"encoding/hex"
)

// Typed string aliases — prevents passing a ProjectID where a NodeID is expected.
type (
	NodeID    string
	EdgeID    string
	ProjectID string
	RunID     string
	TurnID    string
	SessionID string
	PluginID  string
	TokenID   string
)

// Node is a property graph node as read from the substrate.
type Node struct {
	ID          NodeID
	ProjectID   ProjectID
	Type        string // symbol | namespace | concept | file | plugin-defined
	Label       string
	CanonicalID string
	SourceClass SourceClass
	PluginID    PluginID
	Properties  map[string]any
	CreatedAt   int64
	UpdatedAt   int64
}

// Edge is a property graph edge as read from the substrate.
type Edge struct {
	ID          EdgeID
	ProjectID   ProjectID
	SourceID    NodeID
	TargetID    NodeID
	Type        string
	SourceClass SourceClass
	Weight      float64 // from edge_weight table, joined at read time
	PluginID    PluginID
	Properties  map[string]any
	CreatedAt   int64
}

// Anchor is a resolved substrate reference.
// The Strategizer produces AnchorRefs (symbolic).
// The activation layer resolves them to Anchors (concrete nodes + edges).
type Anchor struct {
	Ref        AnchorRef
	Node       *Node           // nil if not resolved to a node
	Edges      []EdgeWithWeight // outbound + inbound edges from this node
	Activation float64
}

// NodeWithActivation pairs a node with its current activation value.
type NodeWithActivation struct {
	Node
	Activation float64
}

// EdgeWithWeight pairs an edge with its current weight and Hebbian metadata.
// Weight shadows Edge.Weight (both reflect the edge_weight table value).
// SourceClass reflects the weight source class (not the edge's structural source class).
// CoActivationCount is the number of times this edge's endpoints co-activated.
type EdgeWithWeight struct {
	Edge
	Weight            float64
	SourceClass       string
	CoActivationCount int
}

// ReferenceResult is a node that references a given substrate node.
// Used by the references tool.
type ReferenceResult struct {
	Node     NodeWithActivation
	EdgeType string
	Weight   float64
}

// OrgMatch is a node found in the org graph matching a cross-project query.
// Used by the crossproject tool.
type OrgMatch struct {
	Node        Node
	ProjectID   ProjectID
	ProjectName string
	Similarity  float64
}

// WeightUpdate carries the data for an edge weight update from Hebbian learning.
type WeightUpdate struct {
	EdgeID            EdgeID
	ProjectID         ProjectID
	NewWeight         float64
	CoActivationDelta int    // increment to co_activation_count
	SourceClass       string // updated source class if promotion occurred
}

// AnchorRef is the symbolic pointer the Strategizer emits.
type AnchorRef struct {
	Type       string // symbol | namespace | concept | file
	ID         string // canonical identifier
	Confidence string // high | medium | low
}

// SourceClass classifies the origin/reliability of a node or edge.
type SourceClass string

const (
	SourceStructural  SourceClass = "structural"
	SourceAssociative SourceClass = "associative"
	SourceSpeculative SourceClass = "speculative"
	SourceDerived     SourceClass = "derived"
)

// NodeID generates a deterministic node ID.
// sha256(projectID + ":" + nodeType + ":" + canonicalID), truncated to 16 bytes, hex-encoded.
func MakeNodeID(projectID, nodeType, canonicalID string) string {
	h := sha256.Sum256([]byte(projectID + ":" + nodeType + ":" + canonicalID))
	return hex.EncodeToString(h[:16])
}

// MakeEdgeID generates a deterministic edge ID.
// sha256(sourceID + ":" + edgeType + ":" + targetID), truncated to 16 bytes, hex-encoded.
func MakeEdgeID(sourceID, edgeType, targetID string) string {
	h := sha256.Sum256([]byte(sourceID + ":" + edgeType + ":" + targetID))
	return hex.EncodeToString(h[:16])
}
