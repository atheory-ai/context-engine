import type { AnalyzerDefinition, Node, Edge } from "@atheory-ai/ce-plugin-sdk"
import { edgeID, nodeID } from "@atheory-ai/ce-plugin-sdk"

/**
 * Promotes class inheritance edges to cross-file relationships when the
 * base class is found elsewhere in the same project.
 *
 * Also emits "annotates" edges for known concepts:
 *   - Classes extending ABC/ABCMeta → "abstract" concept
 *   - Classes decorated with @dataclass → "dataclass" concept
 *   - Classes ending in Error/Exception → "exception" concept
 *   - Classes with __enter__/__exit__ methods → "context-manager" concept
 */
export const classHierarchyAnalyzer: AnalyzerDefinition = {
  name:        "class-hierarchy",
  description: "Resolve cross-file class inheritance and annotate known patterns",

  analyze(nodes: Node[]): Edge[] {
    const edges: Edge[] = []
    const seen  = new Set<string>()

    // Build map: class label → node ID (for cross-file resolution)
    const classByName = new Map<string, string>()
    for (const n of nodes) {
      if (n.type === "symbol" && n.properties["kind"] === "class") {
        // Store by label; last-registered wins (same name in multiple files is ambiguous)
        classByName.set(n.label as string, n.id)
      }
    }

    // Concept node IDs (seeded by engine from our conceptSeeds)
    const abstractConceptID     = nodeID("", "concept", "abstract")
    const dataclassConceptID    = nodeID("", "concept", "dataclass")
    const exceptionConceptID    = nodeID("", "concept", "exception")
    const ctxManagerConceptID   = nodeID("", "concept", "context-manager")

    // Collect dunder methods per file for context-manager annotation
    const fileDunders = new Map<string, Set<string>>() // file_path → set of dunder names
    for (const n of nodes) {
      if (n.type !== "symbol" || n.properties["kind"] !== "method") continue
      const filePath = n.properties["file_path"] as string | undefined
      if (!filePath) continue
      const label = n.label as string
      if (!label.startsWith("__") || !label.endsWith("__")) continue
      const existing = fileDunders.get(filePath) ?? new Set<string>()
      existing.add(label)
      fileDunders.set(filePath, existing)
    }

    for (const n of nodes) {
      if (n.type !== "symbol") continue
      if (n.properties["kind"] !== "class") continue

      const filePath = n.properties["file_path"] as string | undefined
      if (!filePath) continue

      // ── Cross-file inheritance resolution ─────────────────────────────────
      const basesRaw = n.properties["bases"] as string[] | undefined
      if (basesRaw && basesRaw.length > 0) {
        for (const base of basesRaw) {
          if (!base || base === "object") continue
          const baseName = base.split(".").pop() ?? base
          const resolvedID = classByName.get(baseName)
          if (!resolvedID || resolvedID === n.id) continue

          const key = `${n.id}→${resolvedID}`
          if (seen.has(key)) continue
          seen.add(key)

          // Replace or supplement speculative extends with a structural one
          edges.push({
            id:          edgeID(n.id, "extends", resolvedID),
            sourceID:    n.id,
            targetID:    resolvedID,
            type:        "extends",
            sourceClass: "structural",
            properties: {
              analyzer:   "class-hierarchy",
              super_name: base,
            },
          })
        }
      }

      // ── Concept annotations ────────────────────────────────────────────────

      // Abstract: extends ABC or ABCMeta
      if (n.properties["is_abstract"]) {
        const key = `${n.id}→abstract`
        if (!seen.has(key)) {
          seen.add(key)
          edges.push({
            id:          edgeID(n.id, "annotates", abstractConceptID),
            sourceID:    n.id,
            targetID:    abstractConceptID,
            type:        "annotates",
            sourceClass: "associative",
            properties:  { analyzer: "class-hierarchy", heuristic: "extends-abc" },
          })
        }
      }

      // Dataclass: decorated with @dataclass
      if (n.properties["is_dataclass"]) {
        const key = `${n.id}→dataclass`
        if (!seen.has(key)) {
          seen.add(key)
          edges.push({
            id:          edgeID(n.id, "annotates", dataclassConceptID),
            sourceID:    n.id,
            targetID:    dataclassConceptID,
            type:        "annotates",
            sourceClass: "associative",
            properties:  { analyzer: "class-hierarchy", heuristic: "dataclass-decorator" },
          })
        }
      }

      // Exception: subclass of Exception / BaseException / *Error / *Exception
      if (n.properties["is_exception"]) {
        const key = `${n.id}→exception`
        if (!seen.has(key)) {
          seen.add(key)
          edges.push({
            id:          edgeID(n.id, "annotates", exceptionConceptID),
            sourceID:    n.id,
            targetID:    exceptionConceptID,
            type:        "annotates",
            sourceClass: "associative",
            properties:  { analyzer: "class-hierarchy", heuristic: "exception-subclass" },
          })
        }
      }

      // Context manager: file defines both __enter__ and __exit__
      const dunders = fileDunders.get(filePath)
      if (dunders && dunders.has("__enter__") && dunders.has("__exit__")) {
        const key = `${n.id}→context-manager`
        if (!seen.has(key)) {
          seen.add(key)
          edges.push({
            id:          edgeID(n.id, "annotates", ctxManagerConceptID),
            sourceID:    n.id,
            targetID:    ctxManagerConceptID,
            type:        "annotates",
            sourceClass: "associative",
            properties:  { analyzer: "class-hierarchy", heuristic: "enter-exit-methods" },
          })
        }
      }
    }

    return edges
  },
}
