import type { PluginDefinition } from "./types.js"

/**
 * definePlugin is the single entry point for all plugin authors.
 *
 * Usage:
 *   import { definePlugin } from "@atheory-ai/ce-plugin-sdk"
 *
 *   export default definePlugin({
 *     id: "com.example.my-plugin",
 *     name: "My Plugin",
 *     version: "1.0.0",
 *     language: { match, extract, concepts },
 *     tools: [{ name, description, activate, execute }],
 *   })
 */
export function definePlugin(definition: PluginDefinition): PluginDefinition {
  if (!definition.id || !definition.id.includes(".")) {
    throw new Error(
      `Plugin id must be reverse-domain format (e.g., "com.example.my-plugin"). Got: "${definition.id}"`
    )
  }

  if (!definition.name) {
    throw new Error("Plugin name is required")
  }

  if (!definition.version || !isValidSemver(definition.version)) {
    throw new Error(
      `Plugin version must be valid semver (e.g., "1.0.0"). Got: "${definition.version}"`
    )
  }

  if (definition.language) {
    if (typeof definition.language.match !== "function") {
      throw new Error("language.match must be a function")
    }
    if (typeof definition.language.extract !== "function") {
      throw new Error("language.extract must be a function")
    }
    if (definition.language.concepts) {
      for (const seed of definition.language.concepts) {
        if (seed.term !== seed.term.toLowerCase()) {
          throw new Error(`Concept terms must be lowercase. Got: "${seed.term}"`)
        }
        if (!seed.term.match(/^[a-z][a-z0-9-]*$/)) {
          throw new Error(
            `Concept terms must be lowercase-hyphenated. Got: "${seed.term}"`
          )
        }
      }
    }
  }

  if (definition.tools) {
    for (const tool of definition.tools) {
      if (!tool.name) {
        throw new Error("Tool name is required")
      }
      if (!tool.description) {
        throw new Error(`Tool "${tool.name}" description is required`)
      }
      if (tool.description.length > 100) {
        throw new Error(
          `Tool "${tool.name}" description exceeds 100 characters (${tool.description.length}). ` +
          `The Strategizer receives this in its prompt — keep it concise.`
        )
      }
      if (typeof tool.activate !== "function") {
        throw new Error(`Tool "${tool.name}" activate must be a function`)
      }
      if (typeof tool.execute !== "function") {
        throw new Error(`Tool "${tool.name}" execute must be a function`)
      }
    }
  }

  if (definition.analyzers) {
    for (const analyzer of definition.analyzers) {
      if (!analyzer.name) {
        throw new Error("Analyzer name is required")
      }
      if (typeof analyzer.analyze !== "function") {
        throw new Error(`Analyzer "${analyzer.name}" analyze must be a function`)
      }
    }
  }

  if (definition.iirRules) {
    // The host is the authoritative validator (internal/iir). Keep this to a
    // shape sanity check so an obvious mistake fails at author time rather than
    // silently shipping an empty pack.
    if (!Array.isArray(definition.iirRules.rules) || definition.iirRules.rules.length === 0) {
      throw new Error("iirRules.rules must be a non-empty array")
    }
    for (const rule of definition.iirRules.rules) {
      if (!rule.id) {
        throw new Error("Each IIR rule requires an id")
      }
      if (!rule.target || !rule.severity) {
        throw new Error(`IIR rule "${rule.id}" requires target and severity`)
      }
    }
  }

  if (definition.semanticPolicies) {
    const pack = definition.semanticPolicies
    if (pack.schemaVersion !== "v1") {
      throw new Error("semanticPolicies.schemaVersion must be v1")
    }
    if (!Array.isArray(pack.policies) || pack.policies.length === 0) {
      throw new Error("semanticPolicies.policies must be a non-empty array")
    }
    for (const policy of pack.policies) {
      if (!policy.id || !policy.version || !policy.phase || !policy.severity) {
        throw new Error("Each semantic policy requires id, version, phase, and severity")
      }
      if (policy.add && (!policy.add.kind || !policy.add.requirement)) {
        throw new Error(`Semantic policy "${policy.id}" has an invalid add obligation`)
      }
		if (policy.when) {
			for (const field of ["claimKinds", "allClaimKinds", "languages"] as const) {
				const values = policy.when[field]
				if (values !== undefined && (!Array.isArray(values) || values.some((value) => !value.trim()))) {
					throw new Error(`Semantic policy "${policy.id}" has invalid when.${field}`)
				}
			}
		}
    }
  }

  setPluginDefinition(definition)
  return definition
}

export function setPluginDefinition(definition: PluginDefinition): void {
  const globalScope = globalThis as typeof globalThis & {
    __ce_plugin_definition?: PluginDefinition
  }
  globalScope.__ce_plugin_definition = definition
}

function isValidSemver(version: string): boolean {
  return /^\d+\.\d+\.\d+(-[\w.]+)?(\+[\w.]+)?$/.test(version)
}
