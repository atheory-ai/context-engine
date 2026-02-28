# Context Engine — Spec 11: Built-in Tools
## Implementation Spec — callgraph, references, crossproject, concepts, filecontext, summary
### Version 1.0 | February 2026

---

> This spec implements all six built-in cognitive loop tools.
> Hand to Claude Code alongside spec-2-packages.md, spec-3-engine-runner.md,
> and spec-10-activation.md.
> Companion: Context Engine PRD v0.5 Section 10. Decisions Log v1.0 Section 6.

---

## 1. Overview

The six built-in tools are the hands of the cognitive loop. They run
concurrently during fan-out, each surfacing a different kind of evidence
from the substrate graph. Their results become the emissions the Synthesizer
uses to construct the final answer.

Each tool implements `core.Tool` and `ToolWithHint` (from Spec 3):

```go
type Tool interface {
    Name()        string
    Description() string
    Activate(ir core.IR) bool
    Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error)
}

type ToolWithHint interface {
    ActivationHint() string
}
```

Tools are read-only. They query the substrate and emit findings.
They may propose new nodes or edges (sent to the Reviewer for approval)
but never write directly.

---

## 2. Package Structure

```
internal/tools/
  callgraph/
    callgraph.go
    callgraph_test.go
  references/
    references.go
    references_test.go
  crossproject/
    crossproject.go
    crossproject_test.go
  concepts/
    concepts.go
    concepts_test.go
  filecontext/
    filecontext.go
    filecontext_test.go
  summary/
    summary.go
    summary_test.go
  shared/
    emit.go       — shared emission helpers
    format.go     — shared formatting helpers
```

---

## 3. Tool: callgraph

Follows call chains through the substrate from anchor points.
Answers questions about execution flow: "what calls X", "what does X call",
"how does data flow from A to B".

### Activation

```go
func (t *CallgraphTool) Activate(ir core.IR) bool {
    // Explicit: Strategizer set the predicate
    if ir.Predicates["callgraph"] == "true" {
        return true
    }
    // Implicit: anchors contain symbol nodes (functions, methods)
    for _, anchor := range ir.Anchors {
        if anchor.Type == "symbol" && anchor.Confidence != "low" {
            return true
        }
    }
    return false
}

func (t *CallgraphTool) ActivationHint() string {
    return "predicate.callgraph=true, or anchors contain symbol nodes with confidence >= medium"
}
```

### Execute

```go
func (t *CallgraphTool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
    var emissions []core.Emission
    var proposedEdges []core.Edge

    for _, anchor := range req.Anchors {
        if anchor.Node == nil {
            continue
        }
        if anchor.Node.Type != "symbol" {
            continue
        }

        // ── Callers: who calls this symbol ────────────────────────────────
        callers, err := t.substrate.GetCallers(ctx, req.ProjectID, anchor.Node.ID, callgraphDepth)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("get callers for %s: %w", anchor.Node.ID, err)
        }

        // ── Callees: what this symbol calls ───────────────────────────────
        callees, err := t.substrate.GetCallees(ctx, req.ProjectID, anchor.Node.ID, callgraphDepth)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("get callees for %s: %w", anchor.Node.ID, err)
        }

        // ── Format findings ────────────────────────────────────────────────
        if len(callers) > 0 || len(callees) > 0 {
            content := formatCallgraph(anchor.Node, callers, callees)
            emissions = append(emissions, core.Emission{
                RunID:   req.RunID,
                TurnID:  req.TurnID,
                Channel: core.ChanThinking,
                Content: content,
                Metadata: map[string]any{
                    "tool":     "callgraph",
                    "symbol":   anchor.Node.CanonicalID,
                    "callers":  len(callers),
                    "callees":  len(callees),
                },
            })
        }

        // ── Propose speculative edges for undiscovered relationships ───────
        // If we find call relationships not in the substrate, propose them.
        // The Reviewer decides whether to add them.
        for _, callee := range callees {
            if !edgeExists(anchor.Edges, anchor.Node.ID, callee.ID, "calls") {
                proposedEdges = append(proposedEdges, core.Edge{
                    ID:          core.EdgeID(core.HashEdge(anchor.Node.ID, "calls", callee.ID)),
                    SourceID:    anchor.Node.ID,
                    TargetID:    callee.ID,
                    Type:        "calls",
                    SourceClass: "speculative",
                    Properties:  map[string]any{"discovered_by": "callgraph_tool"},
                })
            }
        }
    }

    return core.ToolResult{
        Emissions:     emissions,
        ProposedEdges: proposedEdges,
    }, nil
}

const callgraphDepth = 3 // traverse up to 3 hops in each direction

func formatCallgraph(
    symbol  *core.Node,
    callers []core.NodeWithActivation,
    callees []core.NodeWithActivation,
) string {
    var b strings.Builder

    b.WriteString(fmt.Sprintf("## Call graph: %s\n\n", symbol.Label))

    if len(callers) > 0 {
        b.WriteString("**Called by:**\n")
        for _, c := range callers {
            b.WriteString(fmt.Sprintf("  - `%s` (activation: %.2f)\n",
                c.CanonicalID, c.Activation))
        }
        b.WriteString("\n")
    }

    if len(callees) > 0 {
        b.WriteString("**Calls:**\n")
        for _, c := range callees {
            b.WriteString(fmt.Sprintf("  - `%s` (activation: %.2f)\n",
                c.CanonicalID, c.Activation))
        }
    }

    return b.String()
}
```

### SubstrateReader additions for callgraph

```go
// GetCallers returns nodes that call the given node, up to maxDepth hops.
// Traverses "calls" edges in reverse direction.
GetCallers(ctx context.Context, projectID ProjectID, nodeID NodeID, maxDepth int) ([]NodeWithActivation, error)

// GetCallees returns nodes that the given node calls, up to maxDepth hops.
// Traverses "calls" edges forward.
GetCallees(ctx context.Context, projectID ProjectID, nodeID NodeID, maxDepth int) ([]NodeWithActivation, error)
```

```sql
-- GetCallees (recursive CTE — SQLite 3.35+ supports this)
WITH RECURSIVE call_chain(node_id, depth) AS (
    -- Base: direct callees
    SELECT e.target_id, 1
    FROM edges e
    JOIN edge_weight ew ON ew.edge_id = e.id
    WHERE e.source_id = ?
      AND e.type = 'calls'
      AND e.project_id = ?
      AND ew.weight > 0.05
    UNION ALL
    -- Recursive: callees of callees
    SELECT e.target_id, cc.depth + 1
    FROM edges e
    JOIN edge_weight ew ON ew.edge_id = e.id
    JOIN call_chain cc ON cc.node_id = e.source_id
    WHERE e.type = 'calls'
      AND e.project_id = ?
      AND ew.weight > 0.05
      AND cc.depth < ?  -- maxDepth
)
SELECT DISTINCT n.*, COALESCE(na.activation, 0.0) as activation
FROM call_chain cc
JOIN nodes n ON n.id = cc.node_id
LEFT JOIN node_activation na ON na.node_id = n.id
ORDER BY activation DESC
LIMIT 50;
```

---

## 4. Tool: references

Finds all references to anchored symbols across the project.
Answers: "where is X used", "what depends on X", "what implements X".

### Activation

```go
func (t *ReferencesTool) Activate(ir core.IR) bool {
    if ir.Predicates["references"] == "true" {
        return true
    }
    // Implicit: thinking mode with symbol anchors — references are
    // almost always useful when investigating a symbol
    if ir.Mode == core.IRModeThinking {
        for _, anchor := range ir.Anchors {
            if anchor.Type == "symbol" || anchor.Type == "namespace" {
                return true
            }
        }
    }
    return false
}

func (t *ReferencesTool) ActivationHint() string {
    return "predicate.references=true, or thinking mode with symbol/namespace anchors"
}
```

### Execute

```go
func (t *ReferencesTool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
    var emissions []core.Emission

    for _, anchor := range req.Anchors {
        if anchor.Node == nil {
            continue
        }
        if anchor.Node.Type != "symbol" && anchor.Node.Type != "namespace" {
            continue
        }

        refs, err := t.substrate.GetReferences(ctx, req.ProjectID, anchor.Node.ID)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("get references for %s: %w",
                anchor.Node.ID, err)
        }

        if len(refs) == 0 {
            continue
        }

        // Group references by type
        grouped := groupReferencesByType(refs)

        content := formatReferences(anchor.Node, grouped)
        emissions = append(emissions, core.Emission{
            RunID:   req.RunID,
            TurnID:  req.TurnID,
            Channel: core.ChanThinking,
            Content: content,
            Metadata: map[string]any{
                "tool":       "references",
                "symbol":     anchor.Node.CanonicalID,
                "ref_count":  len(refs),
            },
        })
    }

    return core.ToolResult{Emissions: emissions}, nil
}

// ReferenceGroup groups references by edge type.
type ReferenceGroup struct {
    EdgeType   string
    References []core.NodeWithActivation
}

func groupReferencesByType(refs []core.ReferenceResult) []ReferenceGroup {
    groups := make(map[string][]core.NodeWithActivation)
    for _, ref := range refs {
        groups[ref.EdgeType] = append(groups[ref.EdgeType], ref.Node)
    }

    // Return in consistent order
    order := []string{"implements", "extends", "imports", "references", "calls"}
    var result []ReferenceGroup
    for _, t := range order {
        if nodes, ok := groups[t]; ok {
            result = append(result, ReferenceGroup{EdgeType: t, References: nodes})
        }
    }
    // Add any edge types not in the standard order
    for t, nodes := range groups {
        if !slices.Contains(order, t) {
            result = append(result, ReferenceGroup{EdgeType: t, References: nodes})
        }
    }
    return result
}

func formatReferences(symbol *core.Node, groups []ReferenceGroup) string {
    var b strings.Builder
    b.WriteString(fmt.Sprintf("## References to: %s\n\n", symbol.Label))

    for _, g := range groups {
        b.WriteString(fmt.Sprintf("**%s** (%d):\n", g.EdgeType, len(g.References)))
        // Cap display at 10 per type — avoid flooding context
        display := g.References
        if len(display) > 10 {
            display = display[:10]
            b.WriteString(fmt.Sprintf("  _(showing 10 of %d)_\n", len(g.References)))
        }
        for _, ref := range display {
            b.WriteString(fmt.Sprintf("  - `%s`\n", ref.CanonicalID))
        }
        b.WriteString("\n")
    }

    return b.String()
}
```

### SubstrateReader addition for references

```go
type ReferenceResult struct {
    Node     NodeWithActivation
    EdgeType string
    Weight   float64
}

// GetReferences returns all nodes that reference the given node.
// Traverses all incoming edges regardless of type.
GetReferences(ctx context.Context, projectID ProjectID, nodeID NodeID) ([]ReferenceResult, error)
```

```sql
SELECT
    n.id, n.type, n.label, n.canonical_id, n.source_class,
    n.properties,
    COALESCE(na.activation, 0.0) as activation,
    e.type as edge_type,
    ew.weight
FROM edges e
JOIN nodes n ON n.id = e.source_id
JOIN edge_weight ew ON ew.edge_id = e.id
LEFT JOIN node_activation na ON na.node_id = n.id
WHERE e.target_id = ?
  AND e.project_id = ?
  AND ew.weight > 0.05
ORDER BY ew.weight DESC, na.activation DESC
LIMIT 100;
```

---

## 5. Tool: crossproject

Traverses the org graph to find relationships across project boundaries.
Answers: "what other projects use X", "is there a shared library for Y",
"what's the org-wide dependency on Z".

### Activation

```go
func (t *CrossProjectTool) Activate(ir core.IR) bool {
    if ir.Predicates["crossproject"] == "true" {
        return true
    }
    // Implicit: concept anchors that span multiple namespaces suggest
    // cross-cutting concerns worth investigating org-wide
    conceptCount := 0
    for _, anchor := range ir.Anchors {
        if anchor.Type == "concept" {
            conceptCount++
        }
    }
    return conceptCount >= 2
}

func (t *CrossProjectTool) ActivationHint() string {
    return "predicate.crossproject=true, or 2+ concept anchors (suggests cross-cutting concern)"
}
```

### Execute

```go
func (t *CrossProjectTool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
    var emissions []core.Emission

    for _, anchor := range req.Anchors {
        if anchor.Node == nil {
            continue
        }

        // Find matching nodes in the org graph
        orgMatches, err := t.substrate.FindInOrgGraph(ctx, anchor.Node.CanonicalID, anchor.Node.Type)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("org graph search for %s: %w",
                anchor.Node.CanonicalID, err)
        }

        if len(orgMatches) == 0 {
            continue
        }

        content := formatCrossProject(anchor.Node, orgMatches)
        emissions = append(emissions, core.Emission{
            RunID:   req.RunID,
            TurnID:  req.TurnID,
            Channel: core.ChanThinking,
            Content: content,
            Metadata: map[string]any{
                "tool":          "crossproject",
                "symbol":        anchor.Node.CanonicalID,
                "org_matches":   len(orgMatches),
            },
        })
    }

    return core.ToolResult{Emissions: emissions}, nil
}

// OrgMatch represents a node found in the org graph from another project.
type OrgMatch struct {
    Node      core.Node
    ProjectID core.ProjectID
    ProjectName string
    Similarity  float64 // 0-1, how similar to the query node
}

func formatCrossProject(node *core.Node, matches []OrgMatch) string {
    var b strings.Builder
    b.WriteString(fmt.Sprintf("## Cross-project: %s\n\n", node.Label))
    b.WriteString(fmt.Sprintf("Found in %d other project(s):\n\n", len(matches)))

    for _, m := range matches {
        b.WriteString(fmt.Sprintf("**%s** (`%s`)\n", m.ProjectName, m.Node.CanonicalID))
        b.WriteString(fmt.Sprintf("  type: %s | similarity: %.0f%%\n\n",
            m.Node.Type, m.Similarity*100))
    }

    return b.String()
}
```

### SubstrateReader addition for crossproject

```go
// FindInOrgGraph searches the org-level substrate for nodes similar to
// the given canonical ID and type across all projects.
FindInOrgGraph(ctx context.Context, canonicalID string, nodeType string) ([]OrgMatch, error)
```

---

## 6. Tool: concepts

Expands concept anchors into related nodes and surfaces the domain
vocabulary relevant to the current query.
Answers: "what concepts are related to X", "what does this term mean
in this codebase", "what code implements this domain concept".

### Activation

```go
func (t *ConceptsTool) Activate(ir core.IR) bool {
    if ir.Predicates["concepts"] == "true" {
        return true
    }
    // Implicit: any concept anchors in the IR
    for _, anchor := range ir.Anchors {
        if anchor.Type == "concept" {
            return true
        }
    }
    return false
}

func (t *ConceptsTool) ActivationHint() string {
    return "predicate.concepts=true, or any concept-type anchors in IR"
}
```

### Execute

```go
func (t *ConceptsTool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
    var emissions []core.Emission

    // Collect concept anchors
    var conceptAnchors []core.Anchor
    for _, anchor := range req.Anchors {
        if anchor.Node != nil && anchor.Node.Type == "concept" {
            conceptAnchors = append(conceptAnchors, anchor)
        }
    }

    if len(conceptAnchors) == 0 {
        return core.ToolResult{}, nil
    }

    // For each concept, find implementing nodes
    for _, anchor := range conceptAnchors {
        // Nodes that implement or relate to this concept
        implementors, err := t.substrate.GetConceptImplementors(
            ctx, req.ProjectID, anchor.Node.ID)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("get concept implementors: %w", err)
        }

        // Related concepts (from ontology)
        seed, err := t.substrate.GetConceptSeed(ctx, req.ProjectID, anchor.Node.CanonicalID)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("get concept seed: %w", err)
        }

        content := formatConcepts(anchor.Node, seed, implementors)
        emissions = append(emissions, core.Emission{
            RunID:   req.RunID,
            TurnID:  req.TurnID,
            Channel: core.ChanThinking,
            Content: content,
            Metadata: map[string]any{
                "tool":          "concepts",
                "concept":       anchor.Node.CanonicalID,
                "implementors":  len(implementors),
            },
        })
    }

    return core.ToolResult{Emissions: emissions}, nil
}

func formatConcepts(
    concept      *core.Node,
    seed         *core.ConceptSeed,
    implementors []core.NodeWithActivation,
) string {
    var b strings.Builder
    b.WriteString(fmt.Sprintf("## Concept: %s\n\n", concept.Label))

    if seed != nil {
        if seed.Definition != "" {
            b.WriteString(fmt.Sprintf("**Definition:** %s\n\n", seed.Definition))
        }
        if len(seed.Related) > 0 {
            b.WriteString(fmt.Sprintf("**Related:** %s\n\n",
                strings.Join(seed.Related, ", ")))
        }
        if len(seed.Synonyms) > 0 {
            b.WriteString(fmt.Sprintf("**Synonyms:** %s\n\n",
                strings.Join(seed.Synonyms, ", ")))
        }
    }

    if len(implementors) > 0 {
        b.WriteString(fmt.Sprintf("**Implemented by** (%d nodes):\n", len(implementors)))
        display := implementors
        if len(display) > 15 {
            display = display[:15]
            b.WriteString(fmt.Sprintf("  _(showing 15 of %d)_\n", len(implementors)))
        }
        for _, impl := range display {
            b.WriteString(fmt.Sprintf("  - `%s` (%s)\n", impl.CanonicalID, impl.Type))
        }
    }

    return b.String()
}
```

---

## 7. Tool: filecontext

Surfaces the full structural context of files containing anchor nodes.
Answers: "what else is in this file", "what does this file import",
"what package does this belong to".

### Activation

```go
func (t *FileContextTool) Activate(ir core.IR) bool {
    if ir.Predicates["filecontext"] == "true" {
        return true
    }
    // Implicit: file-type anchors
    for _, anchor := range ir.Anchors {
        if anchor.Type == "file" {
            return true
        }
    }
    return false
}

func (t *FileContextTool) ActivationHint() string {
    return "predicate.filecontext=true, or file-type anchors in IR"
}
```

### Execute

```go
func (t *FileContextTool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
    var emissions []core.Emission

    // Collect unique files — from explicit file anchors and from
    // the files containing activated symbol/namespace nodes
    fileSet := make(map[string]struct{})

    for _, anchor := range req.Anchors {
        if anchor.Node == nil {
            continue
        }
        if anchor.Node.Type == "file" {
            fileSet[anchor.Node.CanonicalID] = struct{}{}
        } else {
            // Find which file this node came from
            filePath := extractFilePath(anchor.Node)
            if filePath != "" {
                fileSet[filePath] = struct{}{}
            }
        }
    }

    for filePath := range fileSet {
        fileNode, err := t.substrate.GetFileNode(ctx, req.ProjectID, filePath)
        if err != nil || fileNode == nil {
            continue
        }

        // All nodes extracted from this file
        fileNodes, err := t.substrate.GetNodesForFile(ctx, req.ProjectID, filePath)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("get nodes for file %s: %w", filePath, err)
        }

        // Imports
        imports, err := t.substrate.GetFileImports(ctx, req.ProjectID, fileNode.ID)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("get imports for %s: %w", filePath, err)
        }

        content := formatFileContext(filePath, fileNodes, imports)
        emissions = append(emissions, core.Emission{
            RunID:   req.RunID,
            TurnID:  req.TurnID,
            Channel: core.ChanThinking,
            Content: content,
            Metadata: map[string]any{
                "tool":        "filecontext",
                "file":        filePath,
                "node_count":  len(fileNodes),
            },
        })
    }

    return core.ToolResult{Emissions: emissions}, nil
}

// extractFilePath infers the file path from a node's canonical ID.
// Go convention: "internal/billing:ProcessPayment" → "internal/billing/"
// Actual file requires a contains edge lookup.
func extractFilePath(node *core.Node) string {
    if node.Type == "file" {
        return node.CanonicalID
    }
    // Use the file property if the plugin set it
    if fp, ok := node.Properties["file_path"].(string); ok {
        return fp
    }
    return ""
}

func formatFileContext(
    filePath  string,
    nodes     []core.Node,
    imports   []core.Node,
) string {
    var b strings.Builder
    b.WriteString(fmt.Sprintf("## File: %s\n\n", filePath))

    // Group nodes by type
    byType := make(map[string][]core.Node)
    for _, n := range nodes {
        byType[n.Type] = append(byType[n.Type], n)
    }

    for _, t := range []string{"symbol", "namespace", "concept"} {
        if nodeGroup, ok := byType[t]; ok {
            b.WriteString(fmt.Sprintf("**%ss** (%d):\n", t, len(nodeGroup)))
            for _, n := range nodeGroup {
                b.WriteString(fmt.Sprintf("  - `%s`\n", n.Label))
            }
            b.WriteString("\n")
        }
    }

    if len(imports) > 0 {
        b.WriteString(fmt.Sprintf("**Imports** (%d):\n", len(imports)))
        for _, imp := range imports {
            b.WriteString(fmt.Sprintf("  - `%s`\n", imp.CanonicalID))
        }
    }

    return b.String()
}
```

---

## 8. Tool: summary

Produces structural summaries of namespace/package nodes.
Answers: "what's in this package", "give me an overview of internal/billing",
"what's the public API of X".

### Activation

```go
func (t *SummaryTool) Activate(ir core.IR) bool {
    if ir.Predicates["summary"] == "true" {
        return true
    }
    // Implicit: namespace-type anchors
    for _, anchor := range ir.Anchors {
        if anchor.Type == "namespace" {
            return true
        }
    }
    return false
}

func (t *SummaryTool) ActivationHint() string {
    return "predicate.summary=true, or namespace-type anchors in IR"
}
```

### Execute

```go
func (t *SummaryTool) Execute(ctx context.Context, req core.ToolRequest) (core.ToolResult, error) {
    var emissions []core.Emission

    for _, anchor := range req.Anchors {
        if anchor.Node == nil {
            continue
        }
        if anchor.Node.Type != "namespace" {
            continue
        }

        // All nodes in this namespace
        members, err := t.substrate.GetNamespaceMembers(ctx, req.ProjectID, anchor.Node.ID)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("get namespace members: %w", err)
        }

        // External dependencies (imports from outside this namespace)
        deps, err := t.substrate.GetNamespaceDependencies(ctx, req.ProjectID, anchor.Node.ID)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("get namespace deps: %w", err)
        }

        // Dependents (who imports this namespace)
        dependents, err := t.substrate.GetNamespaceDependents(ctx, req.ProjectID, anchor.Node.ID)
        if err != nil {
            return core.ToolResult{}, fmt.Errorf("get namespace dependents: %w", err)
        }

        content := formatNamespaceSummary(anchor.Node, members, deps, dependents)
        emissions = append(emissions, core.Emission{
            RunID:   req.RunID,
            TurnID:  req.TurnID,
            Channel: core.ChanThinking,
            Content: content,
            Metadata: map[string]any{
                "tool":       "summary",
                "namespace":  anchor.Node.CanonicalID,
                "members":    len(members),
                "deps":       len(deps),
                "dependents": len(dependents),
            },
        })
    }

    return core.ToolResult{Emissions: emissions}, nil
}

// NamespaceMember groups namespace members by role.
type NamespaceSummary struct {
    ExportedSymbols []core.Node
    InternalSymbols []core.Node
    Types           []core.Node
    Interfaces      []core.Node
    Files           []core.Node
}

func formatNamespaceSummary(
    ns         *core.Node,
    members    []core.Node,
    deps       []core.Node,
    dependents []core.Node,
) string {
    var b strings.Builder
    b.WriteString(fmt.Sprintf("## Package: %s\n\n", ns.CanonicalID))

    // Categorize members
    var exported, internal, types, interfaces []core.Node
    for _, m := range members {
        if m.Type == "symbol" {
            label := m.Label
            if len(label) > 0 && label[0] >= 'A' && label[0] <= 'Z' {
                exported = append(exported, m)
            } else {
                internal = append(internal, m)
            }
        } else if m.Type == "type" {
            types = append(types, m)
        } else if m.Type == "interface" {
            interfaces = append(interfaces, m)
        }
    }

    if len(exported) > 0 {
        b.WriteString(fmt.Sprintf("**Exported symbols** (%d):\n", len(exported)))
        for _, s := range exported {
            b.WriteString(fmt.Sprintf("  - `%s`\n", s.Label))
        }
        b.WriteString("\n")
    }

    if len(interfaces) > 0 {
        b.WriteString(fmt.Sprintf("**Interfaces** (%d):\n", len(interfaces)))
        for _, i := range interfaces {
            b.WriteString(fmt.Sprintf("  - `%s`\n", i.Label))
        }
        b.WriteString("\n")
    }

    if len(types) > 0 {
        b.WriteString(fmt.Sprintf("**Types** (%d):\n", len(types)))
        for _, t := range types {
            b.WriteString(fmt.Sprintf("  - `%s`\n", t.Label))
        }
        b.WriteString("\n")
    }

    if len(deps) > 0 {
        b.WriteString(fmt.Sprintf("**Depends on** (%d packages):\n", len(deps)))
        for _, d := range deps {
            b.WriteString(fmt.Sprintf("  - `%s`\n", d.CanonicalID))
        }
        b.WriteString("\n")
    }

    if len(dependents) > 0 {
        b.WriteString(fmt.Sprintf("**Used by** (%d packages):\n", len(dependents)))
        for _, d := range dependents {
            b.WriteString(fmt.Sprintf("  - `%s`\n", d.CanonicalID))
        }
    }

    return b.String()
}
```

---

## 9. Additional SubstrateReader Methods

All additional methods required by the six tools. Add to
`internal/core/interfaces.go` and implement in `internal/graph/substrate/reader.go`.

```go
// For callgraph tool
GetCallers(ctx context.Context, projectID ProjectID, nodeID NodeID, maxDepth int) ([]NodeWithActivation, error)
GetCallees(ctx context.Context, projectID ProjectID, nodeID NodeID, maxDepth int) ([]NodeWithActivation, error)

// For references tool
GetReferences(ctx context.Context, projectID ProjectID, nodeID NodeID) ([]ReferenceResult, error)

// For crossproject tool
FindInOrgGraph(ctx context.Context, canonicalID string, nodeType string) ([]OrgMatch, error)

// For concepts tool
GetConceptImplementors(ctx context.Context, projectID ProjectID, conceptNodeID NodeID) ([]NodeWithActivation, error)
GetConceptSeed(ctx context.Context, projectID ProjectID, term string) (*ConceptSeed, error)

// For filecontext tool
GetFileNode(ctx context.Context, projectID ProjectID, filePath string) (*Node, error)
GetFileImports(ctx context.Context, projectID ProjectID, fileNodeID NodeID) ([]Node, error)

// For summary tool
GetNamespaceMembers(ctx context.Context, projectID ProjectID, namespaceNodeID NodeID) ([]Node, error)
GetNamespaceDependencies(ctx context.Context, projectID ProjectID, namespaceNodeID NodeID) ([]Node, error)
GetNamespaceDependents(ctx context.Context, projectID ProjectID, namespaceNodeID NodeID) ([]Node, error)
```

---

## 10. Tool Request and Result — Complete Types

```go
// internal/core/interfaces.go (complete ToolRequest and ToolResult)

// ToolRequest is passed to every tool's Execute() function.
type ToolRequest struct {
    RunID     RunID
    TurnID    TurnID
    LoopIndex int
    IR        IR
    Anchors   []Anchor      // top-K activated nodes with edges
    ProjectID ProjectID
    Substrate SubstrateReader
}

// ToolResult is what every tool returns.
type ToolResult struct {
    // Emissions are added to the accumulated run emissions.
    // Thinking/action emissions are shown in the TUI/CLI.
    Emissions []Emission

    // ProposedNodes are sent to the Reviewer for approval.
    // If approved, they are written to the substrate.
    ProposedNodes []Node

    // ProposedEdges are sent to the Reviewer for approval.
    // If approved, they are written to the substrate.
    ProposedEdges []Edge
}
```

---

## 11. Tool Registration in fanout.go

The `buildToolList()` function in `internal/runner/fanout.go` is the
authoritative registration point. All six built-in tools are registered here.

```go
// internal/runner/fanout.go (complete buildToolList)

func buildToolList(substrate core.SubstrateReader, reg *plugins.Registry) []core.Tool {
    tools := []core.Tool{
        callgraph.New(substrate),
        references.New(substrate),
        crossproject.New(substrate),
        concepts.New(substrate),
        filecontext.New(substrate),
        summary.New(substrate),
    }

    // Plugin-contributed tools appended after built-ins
    for _, plugin := range reg.Loaded() {
        tools = append(tools, plugin.Tools()...)
    }

    return tools
}
```

---

## 12. Emission Content Limits

Tool emissions are accumulated across all loop iterations and passed to
the Synthesizer. To prevent context window exhaustion from verbose tool
output, each tool enforces content limits:

| Tool | Max nodes displayed | Max content length |
|------|--------------------|--------------------|
| callgraph | 50 per symbol (callers + callees) | 2000 chars |
| references | 10 per ref type | 2000 chars |
| crossproject | 20 matches | 1500 chars |
| concepts | 15 implementors | 1500 chars |
| filecontext | All nodes in file | 2000 chars |
| summary | All exported + 20 internal | 2000 chars |

Implement in each `formatX()` function. If content would exceed limit,
truncate and append `_(truncated — N more items)_`.

---

## 13. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| Tool interface | core.Tool + optional ToolWithHint |
| Tool writes | None — tools are read-only, propose via ProposedNodes/Edges |
| Callgraph depth | 3 hops in each direction |
| Callgraph SQL | Recursive CTE (SQLite 3.35+) |
| References limit | 100 per node, 10 displayed per type |
| Crossproject search | Org graph only — not all project graphs |
| Concept expansion | Implementors + seed definition + related terms |
| File path inference | file_path property on node, then canonical ID prefix |
| Namespace categorization | Exported (uppercase) vs internal (lowercase) for Go convention |
| Emission limits | Per-tool limits prevent context window exhaustion |
| Tool registration | buildToolList() in fanout.go — single registration point |
| Plugin tools | Appended after built-ins in buildToolList() |

---

*Spec 11: Built-in Tools — v1.0 — February 2026*
*Next: Spec 12 — LLM Provider, Router, Reviewer & Synthesizer Prompts*
*Companion: Context Engine PRD v0.5 Section 10 | Decisions Log v1.0 Section 6*
