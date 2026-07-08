import type { LanguageDefinition } from "@atheory-ai/ce-plugin-sdk"

export const match: LanguageDefinition["match"] = (filePath: string): boolean => {
  if (!filePath.endsWith(".go")) return false
  if (filePath.includes("/vendor/")) return false
  if (filePath.endsWith(".pb.go")) return false
  if (filePath.endsWith("_test.go")) return false
  return true
}
