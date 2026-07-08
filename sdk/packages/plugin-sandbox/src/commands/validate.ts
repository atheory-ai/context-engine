import { PluginLoader } from "../runner/loader.js"
import { renderReport } from "../output/render.js"
import { buildReport } from "../output/report.js"
import { createHash } from "crypto"
import { readFileSync } from "fs"

export interface ValidateOptions {
  json:  boolean
  ce:    string
}

export async function runValidate(wasmPath: string, opts: ValidateOptions): Promise<void> {
  const loader = new PluginLoader(opts.ce)

  const wasmBytes = readFileSync(wasmPath)
  const wasmHash  = createHash("sha256").update(wasmBytes).digest("hex")

  const result = loader.validate(wasmPath)

  const report = buildReport({
    pluginName:       wasmPath,
    pluginVersion:    "unknown",
    wasmHash,
    fixtureResults:   [],
    validationErrors: result.errors,
  })

  if (opts.json) {
    console.log(JSON.stringify(report, null, 2))
  } else {
    renderReport(report)
  }

  if (!result.passed) process.exit(1)
}
