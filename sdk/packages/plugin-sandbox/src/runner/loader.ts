import { spawnSync } from "child_process"
import { writeFileSync, unlinkSync } from "fs"
import { tmpdir } from "os"
import { join } from "path"
import type { ExtractionResult } from "@atheory-ai/ce-plugin-sdk"

export class PluginLoader {
  private ceBinary: string

  constructor(ceBinary = "ce") {
    this.ceBinary = ceBinary
    this.validateBinary()
  }

  private validateBinary(): void {
    // No shell: run the binary directly with an argv array so a ceBinary value
    // like "foo; rm -rf ~" can't be interpreted as a shell command.
    const result = spawnSync(this.ceBinary, ["version"], { stdio: "pipe" })
    if (result.error || result.status !== 0) {
      throw new Error(
        `CE binary not found: ${this.ceBinary}\n` +
        `Install it or specify path with --ce flag.`
      )
    }
  }

  validate(wasmPath: string): { passed: boolean; errors: string[]; output: string } {
    const result = spawnSync(this.ceBinary, ["plugin", "validate", wasmPath, "--json"], {
      encoding: "utf8",
    })

    if (result.status !== 0) {
      return { passed: false, errors: [result.stderr || "validation failed"], output: result.stdout }
    }

    try {
      const parsed = JSON.parse(result.stdout) as Record<string, unknown>
      return { passed: true, errors: [], output: result.stdout, ...parsed }
    } catch {
      return { passed: true, errors: [], output: result.stdout }
    }
  }

  extract(wasmPath: string, fixturePath: string, content: string): ExtractionResult {
    const tmpInput = join(tmpdir(), `ce-sandbox-${Date.now()}.json`)
    writeFileSync(tmpInput, JSON.stringify({ filePath: fixturePath, content }), "utf8")

    const result = spawnSync(
      this.ceBinary,
      ["plugin", "extract", wasmPath, "--input", tmpInput, "--json"],
      { encoding: "utf8" }
    )

    try { unlinkSync(tmpInput) } catch { /* ignore */ }

    if (result.status !== 0) {
      throw new Error(`Extraction failed: ${result.stderr}`)
    }

    return JSON.parse(result.stdout) as ExtractionResult
  }
}
