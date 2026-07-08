import { runValidate } from "./commands/validate.js"
import { runCoverage } from "./commands/coverage.js"
import { runDiff }     from "./commands/diff.js"
import { runExtract }  from "./commands/run.js"

const args = process.argv.slice(2)

function flag(name: string): boolean {
  return args.includes(`--${name}`)
}

function option(name: string, defaultVal: string): string {
  const idx = args.indexOf(`--${name}`)
  return idx !== -1 && args[idx + 1] ? args[idx + 1] : defaultVal
}

const json     = flag("json")
const fixtures = option("fixtures", "./tests/fixtures")
const ce       = option("ce", "ce")

const [command, ...rest] = args.filter(a => !a.startsWith("--"))

switch (command) {
  case "validate": {
    const [wasmPath] = rest
    if (!wasmPath) { console.error("Usage: ce-sandbox validate <file.wasm>"); process.exit(1) }
    runValidate(wasmPath, { json, ce }).catch(die)
    break
  }
  case "coverage": {
    const [wasmPath] = rest
    if (!wasmPath) { console.error("Usage: ce-sandbox coverage <file.wasm>"); process.exit(1) }
    runCoverage(wasmPath, { json, fixtures, ce }).catch(die)
    break
  }
  case "diff": {
    const [oldWasm, newWasm] = rest
    if (!oldWasm || !newWasm) {
      console.error("Usage: ce-sandbox diff <old.wasm> <new.wasm>")
      process.exit(1)
    }
    runDiff(oldWasm, newWasm, { json, fixtures, ce }).catch(die)
    break
  }
  case "run": {
    const [wasmPath, fixturePath] = rest
    if (!wasmPath || !fixturePath) {
      console.error("Usage: ce-sandbox run <file.wasm> <fixture>")
      process.exit(1)
    }
    runExtract(wasmPath, fixturePath, { json, ce }).catch(die)
    break
  }
  default:
    console.log(`
ce-sandbox — Context Engine Plugin Sandbox

Commands:
  validate <file.wasm>               Validate a plugin file
  coverage <file.wasm>               Run coverage analysis against fixtures
  diff <old.wasm> <new.wasm>         Show extraction changes between builds
  run <file.wasm> <fixture>          Run extraction on a single file

Flags:
  --json        Output machine-readable JSON
  --fixtures    Path to fixtures directory (default: ./tests/fixtures)
  --ce          Path to ce binary (default: looks in PATH)
`)
    process.exit(0)
}

function die(err: unknown): never {
  console.error(err instanceof Error ? err.message : String(err))
  process.exit(1)
}
