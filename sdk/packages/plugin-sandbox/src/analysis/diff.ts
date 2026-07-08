import type { ExtractionResult } from "@atheory-ai/ce-plugin-sdk"

export interface ExtractionDiff {
  fixture:       string
  addedNodes:    string[]
  removedNodes:  string[]
  addedEdges:    string[]
  removedEdges:  string[]
  coverageDelta: number
}

export function diffExtractions(
  fixture:     string,
  oldResult:   ExtractionResult,
  newResult:   ExtractionResult,
  oldCoverage: number,
  newCoverage: number,
): ExtractionDiff {
  const oldNodeIDs = new Set(oldResult.nodes.map(n => n.canonicalID))
  const newNodeIDs = new Set(newResult.nodes.map(n => n.canonicalID))

  const oldEdgeIDs = new Set(oldResult.edges.map(e => e.id))
  const newEdgeIDs = new Set(newResult.edges.map(e => e.id))

  return {
    fixture,
    addedNodes:   [...newNodeIDs].filter(id => !oldNodeIDs.has(id)),
    removedNodes: [...oldNodeIDs].filter(id => !newNodeIDs.has(id)),
    addedEdges:   [...newEdgeIDs].filter(id => !oldEdgeIDs.has(id)),
    removedEdges: [...oldEdgeIDs].filter(id => !newEdgeIDs.has(id)),
    coverageDelta: newCoverage - oldCoverage,
  }
}
