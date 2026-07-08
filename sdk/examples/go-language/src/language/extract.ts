import type { LanguageDefinition, ExtractionResult, Node, Edge } from "@atheory-ai/ce-plugin-sdk"
import { nodeID, edgeID } from "@atheory-ai/ce-plugin-sdk"

export const extract: LanguageDefinition["extract"] = (
  filePath: string,
  content:  string,
): ExtractionResult => {
  const nodes: Node[] = []
  const edges: Edge[] = []
  const lines = content.split("\n")

  // Detect package name
  let packageName = ""
  const pkgMatch = content.match(/^package\s+(\w+)/m)
  if (pkgMatch) packageName = pkgMatch[1]

  // File node
  const fileNode: Node = {
    id:          nodeID("", "file", filePath),
    type:        "file",
    label:       filePath,
    canonicalID: filePath,
    sourceClass: "structural",
    properties:  { package: packageName, lineCount: lines.length },
  }
  nodes.push(fileNode)

  // Namespace (package) node
  if (packageName) {
    const nsCanonical = derivePackagePath(filePath, packageName)
    const nsNode: Node = {
      id:          nodeID("", "namespace", nsCanonical),
      type:        "namespace",
      label:       packageName,
      canonicalID: nsCanonical,
      sourceClass: "structural",
      properties:  {},
    }
    nodes.push(nsNode)
    edges.push({
      id:          edgeID(fileNode.id, "belongs_to", nsNode.id),
      sourceID:    fileNode.id,
      targetID:    nsNode.id,
      type:        "belongs_to",
      sourceClass: "structural",
      properties:  {},
    })
  }

  // Imports
  const importBlock = content.match(/import\s*\(([^)]+)\)/s)
  if (importBlock) {
    const importLines = importBlock[1].split("\n")
    for (const imp of importLines) {
      const m = imp.match(/"([^"]+)"/)
      if (m) {
        const importPath = m[1]
        const impNode: Node = {
          id:          nodeID("", "namespace", importPath),
          type:        "namespace",
          label:       importPath.split("/").pop() ?? importPath,
          canonicalID: importPath,
          sourceClass: "structural",
          properties:  {},
        }
        nodes.push(impNode)
        edges.push({
          id:          edgeID(fileNode.id, "imports", impNode.id),
          sourceID:    fileNode.id,
          targetID:    impNode.id,
          type:        "imports",
          sourceClass: "structural",
          properties:  {},
        })
      }
    }
  }

  // Single-line imports
  const singleImports = content.matchAll(/^import\s+"([^"]+)"/gm)
  for (const m of singleImports) {
    const importPath = m[1]
    const impNode: Node = {
      id:          nodeID("", "namespace", importPath),
      type:        "namespace",
      label:       importPath.split("/").pop() ?? importPath,
      canonicalID: importPath,
      sourceClass: "structural",
      properties:  {},
    }
    nodes.push(impNode)
    edges.push({
      id:          edgeID(fileNode.id, "imports", impNode.id),
      sourceID:    fileNode.id,
      targetID:    impNode.id,
      type:        "imports",
      sourceClass: "structural",
      properties:  {},
    })
  }

  // Functions and methods
  const fnRegex = /^func\s+(?:\(([^)]+)\)\s+)?(\w+)\s*\(([^)]*)\)([^{]*)\{/gm
  for (const m of content.matchAll(fnRegex)) {
    const receiver = m[1] ? m[1].trim() : null
    const name     = m[2]
    const _params  = m[3]

    // Determine canonical ID
    let canonical: string
    let receiverType: string | null = null

    if (receiver) {
      // Extract type from receiver like "*MyType" or "MyType"
      const rMatch = receiver.match(/\*?(\w+)$/)
      receiverType = rMatch ? rMatch[1] : null
      canonical = receiverType
        ? `${derivePackagePath(filePath, packageName)}:${receiverType}.${name}`
        : `${derivePackagePath(filePath, packageName)}:${name}`
    } else {
      canonical = `${derivePackagePath(filePath, packageName)}:${name}`
    }

    const fnNode: Node = {
      id:          nodeID("", "symbol", canonical),
      type:        "symbol",
      label:       name,
      canonicalID: canonical,
      sourceClass: "structural",
      properties:  {
        kind:     receiver ? "method" : "function",
        receiver: receiverType ?? undefined,
      },
    }
    nodes.push(fnNode)

    // File → defines → function
    edges.push({
      id:          edgeID(fileNode.id, "defines", fnNode.id),
      sourceID:    fileNode.id,
      targetID:    fnNode.id,
      type:        "defines",
      sourceClass: "structural",
      properties:  {},
    })
  }

  // Type declarations (struct, interface, type alias)
  const typeRegex = /^type\s+(\w+)\s+(struct|interface|\w[^\s{]*)/gm
  for (const m of content.matchAll(typeRegex)) {
    const name     = m[1]
    const kind     = m[2]
    const canonical = `${derivePackagePath(filePath, packageName)}:${name}`

    const typeNode: Node = {
      id:          nodeID("", "symbol", canonical),
      type:        "symbol",
      label:       name,
      canonicalID: canonical,
      sourceClass: "structural",
      properties:  { kind },
    }
    nodes.push(typeNode)

    edges.push({
      id:          edgeID(fileNode.id, "defines", typeNode.id),
      sourceID:    fileNode.id,
      targetID:    typeNode.id,
      type:        "defines",
      sourceClass: "structural",
      properties:  {},
    })
  }

  // Deduplicate nodes by id
  const seen = new Set<string>()
  const uniqueNodes = nodes.filter(n => {
    if (seen.has(n.id)) return false
    seen.add(n.id)
    return true
  })

  const seenEdges = new Set<string>()
  const uniqueEdges = edges.filter(e => {
    if (seenEdges.has(e.id)) return false
    seenEdges.add(e.id)
    return true
  })

  return { nodes: uniqueNodes, edges: uniqueEdges }
}

function derivePackagePath(filePath: string, packageName: string): string {
  // filePath is like "cmd/ce/main.go" → "cmd/ce"
  const dir = filePath.substring(0, filePath.lastIndexOf("/"))
  return dir || packageName
}
