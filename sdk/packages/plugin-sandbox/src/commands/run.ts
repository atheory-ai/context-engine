import { PluginLoader } from "../runner/loader.js"
import { readFileSync } from "fs"
import chalk from "chalk"

export interface RunOptions {
  json: boolean
  ce:   string
}

export async function runExtract(
  wasmPath:    string,
  fixturePath: string,
  opts:        RunOptions,
): Promise<void> {
  const loader  = new PluginLoader(opts.ce)
  const content = readFileSync(fixturePath, "utf8")

  const result = loader.extract(wasmPath, fixturePath, content)

  if (opts.json) {
    console.log(JSON.stringify(result, null, 2))
    return
  }

  console.log()
  console.log(chalk.bold(`Extraction: ${fixturePath}`))
  console.log()
  console.log(chalk.bold(`Nodes (${result.nodes.length})`))
  for (const node of result.nodes) {
    console.log(`  ${chalk.cyan(node.type.padEnd(12))} ${node.label}`)
    console.log(chalk.dim(`               ${node.canonicalID}`))
  }

  console.log()
  console.log(chalk.bold(`Edges (${result.edges.length})`))
  for (const edge of result.edges) {
    console.log(`  ${chalk.yellow(edge.type.padEnd(12))} ${edge.sourceID} → ${edge.targetID}`)
  }
  console.log()
}
