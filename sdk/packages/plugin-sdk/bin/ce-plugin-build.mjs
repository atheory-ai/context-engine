#!/usr/bin/env node
// Build a production CE plugin. The result is a self-contained Extism JS PDK
// module using CE's byte input/output ABI; it contains no development Javy I/O.

import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { createHash } from "node:crypto"
import { dirname, join, resolve } from "node:path"
import { tmpdir } from "node:os"
import { fileURLToPath } from "node:url"
import { spawnSync } from "node:child_process"
import * as esbuild from "esbuild"

const args = parseArgs(process.argv.slice(2))
const pluginDir = resolve(args.plugin ?? process.cwd())
const entry = resolve(pluginDir, args.entry ?? "src/index.ts")
const output = resolve(pluginDir, args.output ?? "dist/plugin.wasm")
const manifestOutput = `${output}.manifest.json`
const here = dirname(fileURLToPath(import.meta.url))
const sdkDir = resolve(here, "..")
const distDir = join(sdkDir, "dist")

if (!existsSync(entry)) fail(`plugin entry does not exist: ${entry}`)
if (!existsSync(join(distDir, "index.js")) || !existsSync(join(distDir, "extism.js"))) {
  fail("SDK is not built; run pnpm --filter @atheory-ai/ce-plugin-sdk build first")
}

const workDir = mkdtempSync(join(tmpdir(), "ce-plugin-build-"))
try {
	const manifestWrapper = join(workDir, "manifest.mjs")
	writeFileSync(manifestWrapper, `
import plugin from ${JSON.stringify(entry)};
import { buildPluginManifest } from ${JSON.stringify(join(distDir, "abi.js"))};
process.stdout.write(JSON.stringify({
  ...buildPluginManifest(plugin),
  abi: { name: "ce-plugin", version: 4, callConvention: "extism-input-output" },
}));
`)
	const manifestBundle = join(workDir, "manifest.bundle.mjs")
	await esbuild.build({
		entryPoints: [manifestWrapper],
		outfile: manifestBundle,
		bundle: true,
		format: "esm",
		platform: "node",
		target: "node20",
		sourcemap: false,
	})
	const manifestResult = spawnSync(process.execPath, [manifestBundle], { encoding: "utf8" })
	if (manifestResult.error) fail(`build plugin manifest: ${manifestResult.error.message}`)
	if (manifestResult.status !== 0) fail(`build plugin manifest: ${manifestResult.stderr || "plugin manifest process failed"}`)
	let manifest
	try {
		manifest = JSON.parse(manifestResult.stdout)
	} catch (error) {
		fail(`parse plugin manifest: ${error instanceof Error ? error.message : String(error)}`)
	}

  const wrapper = join(workDir, "main.mjs")
  writeFileSync(wrapper, `
import plugin from ${JSON.stringify(entry)};
import { setPluginDefinition } from ${JSON.stringify(join(distDir, "index.js"))};
import * as abi from ${JSON.stringify(join(distDir, "extism.js"))};
setPluginDefinition(plugin);
export const ce_plugin_manifest = abi.ce_plugin_manifest;
export const ce_language_match = abi.ce_language_match;
export const ce_language_extract = abi.ce_language_extract;
export const ce_language_concepts = abi.ce_language_concepts;
export const ce_analyzers_list = abi.ce_analyzers_list;
export const ce_analyzer_run = abi.ce_analyzer_run;
export const ce_tools_list = abi.ce_tools_list;
export const ce_tool_activate = abi.ce_tool_activate;
export const ce_tool_execute = abi.ce_tool_execute;
`)
  const bundle = join(workDir, "plugin.mjs")
  await esbuild.build({
    entryPoints: [wrapper],
    outfile: bundle,
    bundle: true,
    format: "cjs",
    platform: "neutral",
    target: "es2020",
    sourcemap: false,
  })
  const dts = join(workDir, "main.d.ts")
  writeFileSync(dts, `declare module "main" {
  export function ce_plugin_manifest(): I32;
  export function ce_language_match(): I32;
  export function ce_language_extract(): I32;
  export function ce_language_concepts(): I32;
  export function ce_analyzers_list(): I32;
  export function ce_analyzer_run(): I32;
  export function ce_tools_list(): I32;
  export function ce_tool_activate(): I32;
  export function ce_tool_execute(): I32;
}
`)
  mkdirSync(dirname(output), { recursive: true })
  const extismJS = process.env.EXTISM_JS ?? "extism-js"
  const result = spawnSync(extismJS, [bundle, "-i", dts, "-o", output], { stdio: "inherit" })
  if (result.error) fail(`run ${extismJS}: ${result.error.message}`)
  if (result.status !== 0) process.exit(result.status ?? 1)
	const wasmSHA256 = createHash("sha256").update(readFileSync(output)).digest("hex")
	writeFileSync(manifestOutput, `${JSON.stringify({ ...manifest, wasm_sha256: wasmSHA256 }, null, 2)}\n`)
	process.stdout.write(`Built production CE plugin: ${output}\n`)
	process.stdout.write(`Wrote CE plugin manifest: ${manifestOutput}\n`)
} finally {
  rmSync(workDir, { recursive: true, force: true })
}

function parseArgs(values) {
  const out = {}
  for (let i = 0; i < values.length; i++) {
    const value = values[i]
    if (value === "--help" || value === "-h") {
      process.stdout.write("Usage: ce-plugin-build [--plugin DIR] [--entry PATH] [--output PATH]\n")
      process.exit(0)
    }
    if (!value.startsWith("--")) fail(`unexpected argument: ${value}`)
    const key = value.slice(2)
    const next = values[++i]
    if (!next || next.startsWith("--")) fail(`missing value for --${key}`)
    out[key] = next
  }
  return out
}

function fail(message) {
  process.stderr.write(`ce-plugin-build: ${message}\n`)
  process.exit(1)
}
