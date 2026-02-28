// Package shared provides utilities shared across all built-in tool packages.
package shared

import (
	"context"

	"github.com/atheory/context-engine/internal/core"
)

// NilSubstrate implements core.SubstrateReader with no-op methods that return
// nil/empty values. Embed this in test mocks to avoid implementing every method.
type NilSubstrate struct{}

func (NilSubstrate) GetNode(_ context.Context, _ core.ProjectID, _ core.NodeID) (*core.Node, error) {
	return nil, nil
}
func (NilSubstrate) GetNodeByCanonicalID(_ context.Context, _ core.ProjectID, _ string) (*core.Node, error) {
	return nil, nil
}
func (NilSubstrate) GetNodesByNamespacePrefix(_ context.Context, _ core.ProjectID, _ string, _ int) ([]core.Node, error) {
	return nil, nil
}
func (NilSubstrate) GetConceptNodes(_ context.Context, _ core.ProjectID, _ string) ([]core.Node, error) {
	return nil, nil
}
func (NilSubstrate) GetNodesForFile(_ context.Context, _ core.ProjectID, _ string) ([]core.Node, error) {
	return nil, nil
}
func (NilSubstrate) GetNodesBySuffix(_ context.Context, _ core.ProjectID, _ string, _ int) ([]core.Node, error) {
	return nil, nil
}
func (NilSubstrate) GetTopKActivated(_ context.Context, _ core.ProjectID, _ int) ([]core.NodeWithActivation, error) {
	return nil, nil
}
func (NilSubstrate) GetEdgesFrom(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.EdgeWithWeight, error) {
	return nil, nil
}
func (NilSubstrate) GetEdgesTo(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.EdgeWithWeight, error) {
	return nil, nil
}
func (NilSubstrate) GetEdgesBetween(_ context.Context, _ core.ProjectID, _, _ core.NodeID) ([]core.EdgeWithWeight, error) {
	return nil, nil
}
func (NilSubstrate) GetConceptSeeds(_ context.Context, _ core.ProjectID) ([]core.ConceptSeed, error) {
	return nil, nil
}
func (NilSubstrate) GetOrgConceptSeeds(_ context.Context) ([]core.ConceptSeed, error) {
	return nil, nil
}
func (NilSubstrate) GetCallers(_ context.Context, _ core.ProjectID, _ core.NodeID, _ int) ([]core.NodeWithActivation, error) {
	return nil, nil
}
func (NilSubstrate) GetCallees(_ context.Context, _ core.ProjectID, _ core.NodeID, _ int) ([]core.NodeWithActivation, error) {
	return nil, nil
}
func (NilSubstrate) GetReferences(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.ReferenceResult, error) {
	return nil, nil
}
func (NilSubstrate) FindInOrgGraph(_ context.Context, _ string, _ string) ([]core.OrgMatch, error) {
	return nil, nil
}
func (NilSubstrate) GetConceptImplementors(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.NodeWithActivation, error) {
	return nil, nil
}
func (NilSubstrate) GetConceptSeed(_ context.Context, _ core.ProjectID, _ string) (*core.ConceptSeed, error) {
	return nil, nil
}
func (NilSubstrate) GetFileNode(_ context.Context, _ core.ProjectID, _ string) (*core.Node, error) {
	return nil, nil
}
func (NilSubstrate) GetFileImports(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.Node, error) {
	return nil, nil
}
func (NilSubstrate) GetNamespaceMembers(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.Node, error) {
	return nil, nil
}
func (NilSubstrate) GetNamespaceDependencies(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.Node, error) {
	return nil, nil
}
func (NilSubstrate) GetNamespaceDependents(_ context.Context, _ core.ProjectID, _ core.NodeID) ([]core.Node, error) {
	return nil, nil
}
