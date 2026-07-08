import { PluginLoader } from "../runner/loader.js"
import { loadFixtures } from "../runner/fixtures.js"
import { computeCoverage } from "../analysis/coverage.js"
import { buildReport } from "../output/report.js"
import { renderReport } from "../output/render.js"
import { createHash } from "crypto"
import { readFileSync } from "fs"
import type { FixtureResult } from "../output/schema.js"

export interface CoverageOptions {
  json:     boolean
  fixtures: string
  ce:       string
}

export async function runCoverage(wasmPath: string, opts: CoverageOptions): Promise<void> {
  const loader   = new PluginLoader(opts.ce)
  const fixtures = loadFixtures(opts.fixtures)

  const wasmBytes = readFileSync(wasmPath)
  const wasmHash  = createHash("sha256").update(wasmBytes).digest("hex")

  // Validate first
  const validation = loader.validate(wasmPath)

  const fixtureResults: FixtureResult[] = []

  for (const fixture of fixtures) {
    let extraction = { nodes: [], edges: [] }

    if (validation.passed) {
      try {
        extraction = loader.extract(wasmPath, fixture.path, fixture.content)
      } catch (err) {
        const msg = err instanceof Error ? err.message : String(err)
        validation.errors.push(`Extraction failed for ${fixture.path}: ${msg}`)
        validation.passed = false
      }
    }

    const cov = computeCoverage(fixture.path, fixture.content, extraction)

    fixtureResults.push({
      fixturePath:    cov.fixturePath,
      astSymbols:     cov.expectedSymbols.length,
      extractedNodes: cov.extractedNodes.length,
      extractedEdges: extraction.edges.length,
      coveragePct:    cov.coveragePct,
      missingSymbols: cov.missingSymbols,
      extraNodes:     cov.extraNodes.map(n => n.canonicalID),
    })
  }

  const report = buildReport({
    pluginName:       wasmPath,
    pluginVersion:    "unknown",
    wasmHash,
    fixtureResults,
    validationErrors: validation.errors,
  })

  if (opts.json) {
    console.log(JSON.stringify(report, null, 2))
  } else {
    renderReport(report)
  }

  if (!report.validation.passed) process.exit(1)
}
