import type { LanguageDefinition } from "@atheory-ai/ce-plugin-sdk"

export const match: LanguageDefinition["match"] = (filePath: string): boolean => {
  // Handle .go files, excluding vendor/ and generated files
  if (!filePath.endsWith(".go")) return false
  if (filePath.includes("/vendor/") || filePath.startsWith("vendor/")) return false
  if (filePath.endsWith(".pb.go")) return false
  return true
}
