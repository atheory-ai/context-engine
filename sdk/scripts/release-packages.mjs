#!/usr/bin/env node

import { existsSync, readFileSync } from "node:fs"
import { join } from "node:path"
import { spawnSync } from "node:child_process"

const packages = {
  "plugin-sdk": { directory: "packages/plugin-sdk" },
  "plugin-sandbox": { directory: "packages/plugin-sandbox" },
  "create-ce-plugin": { directory: "packages/create-ce-plugin" },
}

const args = new Set(process.argv.slice(2))
const publish = args.has("--publish")
const dryRun = args.has("--dry-run") || !publish
const packageIndex = process.argv.indexOf("--package")
const packageName = packageIndex === -1 ? "all" : process.argv[packageIndex + 1]
const tagIndex = process.argv.indexOf("--tag")
const npmTag = tagIndex === -1 ? "latest" : process.argv[tagIndex + 1]

if (args.has("--help") || args.has("-h")) {
  console.log(`Usage:
  pnpm release:dry-run
  pnpm release:dry-run -- --package plugin-sdk
  pnpm release:publish -- --package plugin-sdk [--tag latest]`)
  process.exit(0)
}
if (publish && args.has("--dry-run")) fail("Choose either --publish or --dry-run, not both.")
if (!npmTag) fail("Missing value for --tag.")
if (!packageName) fail("Missing value for --package.")
if (publish && packageName === "all") fail("Publishing requires one explicit --package value.")

const selected = packageName === "all"
  ? Object.entries(packages)
  : [[packageName, packages[packageName]]]

for (const [id, definition] of selected) {
  if (!definition) fail(`Unknown package "${id}". Choose one of: ${Object.keys(packages).join(", ")}.`)
  const manifest = JSON.parse(readFileSync(join(definition.directory, "package.json"), "utf8"))
  validateManifest(manifest, definition.directory)

  const command = ["publish", "--access", "public", "--tag", npmTag, "--no-git-checks"]
  if (dryRun) command.push("--dry-run")
  if (publish) command.push("--provenance")

  console.log(`\n${dryRun ? "Dry-running" : "Publishing"} ${manifest.name}@${manifest.version}`)
  const result = spawnSync("pnpm", command, { cwd: definition.directory, stdio: "inherit" })
  if (result.status !== 0) process.exit(result.status ?? 1)
}

function validateManifest(manifest, directory) {
  if (manifest.private) fail(`${manifest.name} is private and cannot be published.`)
  if (manifest.publishConfig?.access !== "public") fail(`${manifest.name} must set publishConfig.access to public.`)
  if (!manifest.version || !manifest.description || !manifest.license) fail(`${manifest.name} must declare version, description, and license.`)
  if (!existsSync(join(directory, "README.md"))) fail(`${manifest.name} is missing README.md.`)
  if (!existsSync(join(directory, "LICENSE"))) fail(`${manifest.name} is missing LICENSE.`)
}

function fail(message) {
  console.error(`release package check failed: ${message}`)
  process.exit(1)
}
