import type { ExtractionResult, Node } from "@atheory-ai/ce-plugin-sdk"
import { enumerateExpectedSymbols, type ExpectedSymbol } from "./ast.js"

export interface FileCoverageResult {
  fixturePath:     string
  expectedSymbols: ExpectedSymbol[]
  extractedNodes:  Node[]
  matchedSymbols:  string[]
  missingSymbols:  string[]
  extraNodes:      Node[]
  coveragePct:     number
}

export function computeCoverage(
  fixturePath: string,
  content:     string,
  extraction:  ExtractionResult,
): FileCoverageResult {
  const expected = enumerateExpectedSymbols(fixturePath, content)

  // null means unsupported file type — coverage is not applicable
  if (expected === null) {
    return {
      fixturePath,
      expectedSymbols: [],
      extractedNodes:  extraction.nodes,
      matchedSymbols:  [],
      missingSymbols:  [],
      extraNodes:      extraction.nodes,
      coveragePct:     -1,
    }
  }

  // known file type but no expected symbols (e.g. empty file)
  if (expected.length === 0) {
    return {
      fixturePath,
      expectedSymbols: [],
      extractedNodes:  extraction.nodes,
      matchedSymbols:  [],
      missingSymbols:  [],
      extraNodes:      extraction.nodes,
      coveragePct:     0,
    }
  }

  const matched: string[] = []
  const missing: string[] = []

  for (const sym of expected) {
    const covered = extraction.nodes.some(
      node =>
        node.label === sym.name ||
        node.canonicalID.endsWith(`:${sym.name}`) ||
        node.canonicalID.endsWith(`/${sym.name}`)
    )
    if (covered) {
      matched.push(sym.name)
    } else {
      missing.push(sym.name)
    }
  }

  const extra = extraction.nodes.filter(
    node => !expected.some(
      sym =>
        node.label === sym.name ||
        node.canonicalID.endsWith(`:${sym.name}`) ||
        node.canonicalID.endsWith(`/${sym.name}`)
    )
  )

  const coveragePct = expected.length > 0
    ? (matched.length / expected.length) * 100
    : 0

  return {
    fixturePath,
    expectedSymbols: expected,
    extractedNodes:  extraction.nodes,
    matchedSymbols:  matched,
    missingSymbols:  missing,
    extraNodes:      extra,
    coveragePct,
  }
}
