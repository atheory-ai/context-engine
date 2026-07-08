import { PluginLoader } from "../runner/loader.js"
import { loadFixtures } from "../runner/fixtures.js"
import { computeCoverage } from "../analysis/coverage.js"
import { diffExtractions } from "../analysis/diff.js"
import { renderReport } from "../output/render.js"
import { buildReport } from "../output/report.js"
import { createHash } from "crypto"
import { readFileSync } from "fs"
import type { ExtractionDiff } from "../analysis/diff.js"
import type { FixtureResult } from "../output/schema.js"

export interface DiffOptions {
  json:     boolean
  fixtures: string
  ce:       string
}

export async function runDiff(
  oldWasmPath: string,
  newWasmPath: string,
  opts: DiffOptions,
): Promise<void> {
  const loader   = new PluginLoader(opts.ce)
  const fixtures = loadFixtures(opts.fixtures)

  const oldBytes = readFileSync(oldWasmPath)
  const newBytes = readFileSync(newWasmPath)
  const oldHash  = createHash("sha256").update(oldBytes).digest("hex")
  const newHash  = createHash("sha256").update(newBytes).digest("hex")

  const fixtureResults: FixtureResult[] = []
  const diffs: ExtractionDiff[] = []
  let oldTotalCoverage = 0
  let newTotalCoverage = 0
  let knownCount = 0

  for (const fixture of fixtures) {
    const oldExtraction = loader.extract(oldWasmPath, fixture.path, fixture.content)
    const newExtraction = loader.extract(newWasmPath, fixture.path, fixture.content)

    const oldCov = computeCoverage(fixture.path, fixture.content, oldExtraction)
    const newCov = computeCoverage(fixture.path, fixture.content, newExtraction)

    if (oldCov.coveragePct >= 0 && newCov.coveragePct >= 0) {
      oldTotalCoverage += oldCov.coveragePct
      newTotalCoverage += newCov.coveragePct
      knownCount++
    }

    const diff = diffExtractions(
      fixture.path,
      oldExtraction,
      newExtraction,
      oldCov.coveragePct,
      newCov.coveragePct,
    )
    diffs.push(diff)

    fixtureResults.push({
      fixturePath:    newCov.fixturePath,
      astSymbols:     newCov.expectedSymbols.length,
      extractedNodes: newCov.extractedNodes.length,
      extractedEdges: newExtraction.edges.length,
      coveragePct:    newCov.coveragePct,
      missingSymbols: newCov.missingSymbols,
      extraNodes:     newCov.extraNodes.map(n => n.canonicalID),
    })
  }

  const coverageDelta = knownCount > 0
    ? (newTotalCoverage - oldTotalCoverage) / knownCount
    : 0

  const report = buildReport({
    pluginName:       newWasmPath,
    pluginVersion:    "unknown",
    wasmHash:         newHash,
    fixtureResults,
    validationErrors: [],
  })

  report.diff = {
    previousWasmHash: oldHash,
    fixtures:         diffs,
    coverageDelta,
  }

  if (opts.json) {
    console.log(JSON.stringify(report, null, 2))
  } else {
    renderReport(report)
  }
}
