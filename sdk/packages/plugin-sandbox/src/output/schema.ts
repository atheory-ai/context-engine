import type { ExtractionDiff } from "../analysis/diff.js"

export interface SandboxReport {
  schemaVersion: 1
  pluginName:    string
  pluginVersion: string
  wasmHash:      string
  builtAt:       number

  fixtureResults: FixtureResult[]

  aggregate: {
    totalExpectedSymbols: number
    totalExtractedNodes:  number
    coveragePct:          number
    totalEdges:           number
  }

  validation: {
    passed:   boolean
    errors:   string[]
    warnings: string[]
  }

  diff?: {
    previousWasmHash: string
    fixtures:         ExtractionDiff[]
    coverageDelta:    number
  }
}

export interface FixtureResult {
  fixturePath:    string
  astSymbols:     number
  extractedNodes: number
  extractedEdges: number
  coveragePct:    number
  missingSymbols: string[]
  extraNodes:     string[]
}
