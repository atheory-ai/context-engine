import chalk from "chalk"
import type { SandboxReport } from "./schema.js"

export function renderReport(report: SandboxReport): void {
  console.log()
  console.log(chalk.bold(`${report.pluginName} v${report.pluginVersion}`))
  console.log(chalk.dim(`wasm: ${report.wasmHash.slice(0, 12)}...`))
  console.log()

  if (report.fixtureResults.length > 0) {
    console.log(chalk.bold("Coverage"))
    console.log()

    for (const f of report.fixtureResults) {
      const pct   = f.coveragePct === -1 ? "N/A" : `${f.coveragePct.toFixed(0)}%`
      const color = f.coveragePct >= 80 ? chalk.green
                  : f.coveragePct >= 50 ? chalk.yellow
                  : chalk.red
      const bar   = coverageBar(f.coveragePct)

      console.log(`  ${f.fixturePath}`)
      console.log(`  ${color(pct.padStart(4))}  ${bar}  ${f.extractedNodes} nodes, ${f.extractedEdges} edges`)

      if (f.missingSymbols.length > 0 && f.missingSymbols.length <= 5) {
        console.log(chalk.dim(`         missing: ${f.missingSymbols.join(", ")}`))
      } else if (f.missingSymbols.length > 5) {
        console.log(chalk.dim(
          `         missing: ${f.missingSymbols.slice(0, 5).join(", ")} +${f.missingSymbols.length - 5} more`
        ))
      }
      console.log()
    }

    const aggPct   = report.aggregate.coveragePct
    const aggColor = aggPct >= 80 ? chalk.green : aggPct >= 50 ? chalk.yellow : chalk.red
    const aggStr   = aggPct < 0 ? "N/A" : `${aggPct.toFixed(1)}%`
    console.log(
      `  ${chalk.bold("Total")}  ${aggColor(aggStr)}  ` +
      `(${report.aggregate.totalExtractedNodes}/${report.aggregate.totalExpectedSymbols} symbols)`
    )
    console.log()
  }

  if (report.diff) {
    const delta = report.diff.coverageDelta
    const sign  = delta > 0 ? "+" : ""
    const color = delta > 0 ? chalk.green : delta < 0 ? chalk.red : chalk.dim
    console.log(chalk.bold("Changes from previous build"))
    console.log(`  Coverage: ${color(`${sign}${delta.toFixed(1)}%`)}`)

    for (const f of report.diff.fixtures) {
      if (f.addedNodes.length > 0 || f.removedNodes.length > 0) {
        console.log(`  ${f.fixture}`)
        if (f.addedNodes.length > 0) {
          console.log(chalk.green(`    + ${f.addedNodes.length} nodes: ${f.addedNodes.slice(0, 3).join(", ")}`))
        }
        if (f.removedNodes.length > 0) {
          console.log(chalk.red(`    - ${f.removedNodes.length} nodes: ${f.removedNodes.slice(0, 3).join(", ")}`))
        }
      }
    }
    console.log()
  }

  if (report.validation.errors.length > 0) {
    for (const err of report.validation.errors) {
      console.log(chalk.red(`  \u2717 ${err}`))
    }
    console.log()
  }

  if (report.validation.warnings.length > 0) {
    for (const warn of report.validation.warnings) {
      console.log(chalk.yellow(`  \u26A0 ${warn}`))
    }
    console.log()
  }

  if (report.validation.passed && report.validation.warnings.length === 0) {
    console.log(chalk.green("  \u2713 All checks passed"))
    console.log()
  }
}

function coverageBar(pct: number): string {
  if (pct < 0) return chalk.dim("\u2591".repeat(10) + " N/A")
  const filled = Math.round(pct / 10)
  const empty  = 10 - filled
  const color  = pct >= 80 ? chalk.green : pct >= 50 ? chalk.yellow : chalk.red
  return color("\u2588".repeat(filled)) + chalk.dim("\u2591".repeat(empty))
}
