import type { LanguageDefinition } from "@atheory-ai/ce-plugin-sdk"

const EXTENSIONS = new Set([".ts", ".tsx", ".js", ".jsx", ".mts", ".cts", ".mjs", ".cjs"])

export const match: LanguageDefinition["match"] = (filePath: string): boolean => {
  const dot = filePath.lastIndexOf(".")
  if (dot < 0) return false
  const ext = filePath.substring(dot)
  if (!EXTENSIONS.has(ext)) return false
  if (filePath.includes("/node_modules/")) return false
  if (filePath.includes("/.next/")) return false
  if (filePath.includes("/dist/")) return false
  if (filePath.endsWith(".d.ts")) return false
  return true
}
