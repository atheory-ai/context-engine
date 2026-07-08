import type { SandboxReport } from "./schema.js"

export function addValidationInsights(report: SandboxReport): void {
  const { aggregate, validation } = report

  if (aggregate.coveragePct >= 0 && aggregate.coveragePct < 80) {
    validation.warnings.push(
      `Coverage is ${aggregate.coveragePct.toFixed(1)}% — below the 80% recommended threshold. ` +
      `Review missing symbols and add extraction patterns for them.`
    )
  }

  for (const f of report.fixtureResults) {
    if (f.extractedNodes === 0 && f.astSymbols > 0) {
      validation.errors.push(
        `Fixture ${f.fixturePath}: plugin extracted 0 nodes from a file ` +
        `with ${f.astSymbols} expected symbols. ` +
        `Check that language.match() returns true for this file extension.`
      )
      validation.passed = false
    }
  }

  if (report.fixtureResults.length === 0) {
    validation.warnings.push(
      `No fixture files found. Add sample files to tests/fixtures/ to enable coverage analysis.`
    )
  }
}

export function buildReport(params: {
  pluginName:     string
  pluginVersion:  string
  wasmHash:       string
  fixtureResults: SandboxReport["fixtureResults"]
  validationErrors: string[]
}): SandboxReport {
  const { pluginName, pluginVersion, wasmHash, fixtureResults, validationErrors } = params

  const totalExpected = fixtureResults.reduce((s, f) => s + f.astSymbols, 0)
  const totalExtracted = fixtureResults.reduce((s, f) => s + f.extractedNodes, 0)
  const totalEdges = fixtureResults.reduce((s, f) => s + f.extractedEdges, 0)

  const knownFixtures = fixtureResults.filter(f => f.coveragePct >= 0)
  const coveragePct = knownFixtures.length > 0
    ? knownFixtures.reduce((s, f) => s + f.coveragePct, 0) / knownFixtures.length
    : -1

  const report: SandboxReport = {
    schemaVersion: 1,
    pluginName,
    pluginVersion,
    wasmHash,
    builtAt: Date.now(),
    fixtureResults,
    aggregate: {
      totalExpectedSymbols: totalExpected,
      totalExtractedNodes:  totalExtracted,
      coveragePct,
      totalEdges,
    },
    validation: {
      passed:   validationErrors.length === 0,
      errors:   [...validationErrors],
      warnings: [],
    },
  }

  addValidationInsights(report)
  return report
}
