import type { AnalyzerDefinition, Node, Edge } from "@atheory-ai/ce-plugin-sdk"
import { edgeID } from "@atheory-ai/ce-plugin-sdk"

/**
 * Resolves relative Python imports to concrete file nodes and emits
 * module-level "depends_on" edges between file nodes.
 *
 * Python relative imports: `from . import x`, `from .. import y`, `from .utils import z`
 * These are promoted to file→file edges when the target module exists in the same project.
 */
export const moduleDepsAnalyzer: AnalyzerDefinition = {
  name:        "module-deps",
  description: "Resolve relative Python imports to file-level dependency edges",

  analyze(nodes: Node[]): Edge[] {
    const edges: Edge[] = []
    const seen  = new Set<string>()

    // Build map: module_path → file node ID
    const fileByModule = new Map<string, string>()
    for (const n of nodes) {
      if (n.type === "namespace") {
        const modulePath = n.properties["module_path"] as string | undefined
        const fileNodeID = n.properties["file_path"] as string | undefined
        if (modulePath && fileNodeID) {
          // Also map the file node itself
        }
      }
      if (n.type === "file") {
        const filePath = n.canonicalID as string
        // Convert file path to module notation
        const modulePath = filePath
          .replace(/\.pyi?w?$/, "")
          .replace(/\//g, ".")
          .replace(/^\.+/, "")
        fileByModule.set(modulePath, n.id)
        // Also store the file path for resolution
        fileByModule.set(filePath, n.id)
      }
    }

    // Find namespace nodes representing relative imports
    for (const n of nodes) {
      if (n.type !== "namespace") continue
      const importPath = n.properties["import_path"] as string | undefined
      const fromFile   = n.properties["from_file"]  as string | undefined
      const isRelative = n.properties["relative"]   as boolean | undefined
      if (!importPath || !fromFile) continue
      if (!isRelative && !importPath.startsWith(".")) continue

      // Resolve relative import to absolute module path
      const resolved = resolveRelativeImport(fromFile, importPath)
      if (!resolved) continue

      // Try exact match, then with __init__
      const candidates = [
        resolved,
        resolved + ".__init__",
      ]

      for (const candidate of candidates) {
        const targetID = fileByModule.get(candidate)
        if (!targetID) continue

        // Get the source file node (the file that owns this namespace)
        const sourceFileID = fileByModule.get(fromFile)
        if (!sourceFileID) break

        const key = `${sourceFileID}→${targetID}`
        if (seen.has(key)) break
        seen.add(key)

        edges.push({
          id:          edgeID(sourceFileID, "depends_on", targetID),
          sourceID:    sourceFileID,
          targetID,
          type:        "depends_on",
          sourceClass: "structural",
          properties: {
            analyzer:    "module-deps",
            import_path: importPath,
            resolved:    candidate,
          },
        })
        break
      }
    }

    return edges
  },
}

/**
 * Resolve a relative Python import to an absolute module path.
 *
 * `from .utils import x` in `src/services/user.py` → `src.services.utils`
 * `from ..models import y` in `src/services/user.py` → `src.models`
 */
function resolveRelativeImport(fromFile: string, importPath: string): string | null {
  if (!importPath.startsWith(".")) {
    return importPath // Already absolute
  }

  // Count leading dots to determine how many package levels to go up
  let dots = 0
  while (dots < importPath.length && importPath[dots] === ".") dots++
  const rest = importPath.slice(dots)

  // Convert file path to package path
  const fileDir = fromFile.substring(0, fromFile.lastIndexOf("/")) || "."
  const parts = fileDir.split("/").filter(p => p !== "." && p !== "")

  // Go up (dots - 1) levels — one dot = current package, two dots = parent
  const upLevels = Math.max(0, dots - 1)
  if (upLevels > parts.length) return null
  const base = parts.slice(0, parts.length - upLevels)

  const resolved = rest ? [...base, ...rest.split(".")].join(".") : base.join(".")
  return resolved || null
}
