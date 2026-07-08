# Designing Cognitive Loop Tools

## What tools do

Tools run during the cognitive loop after the Strategizer emits an IR.
A tool receives the IR and a substrate client, then returns:
- `emissions` — messages sent to thinking/action/debug channels
- `proposedNodes` — new nodes to add to the graph
- `proposedEdges` — new edges to add to the graph

## activate() must be pure

```typescript
// CORRECT — pure predicate
activate(ir) {
  return ir.predicates["call-graph"] === "true"
}

// CORRECT — anchor-based activation
activate(ir) {
  return ir.anchors.some(a => a.type === "symbol")
}

// WRONG — side effect
activate(ir) {
  log.debug("checking activation")  // BANNED
  return true
}

// WRONG — assignment
activate(ir) {
  this.lastIR = ir  // BANNED
  return true
}
```

## description: max 100 characters

The description is injected into the Strategizer's prompt.
Write it as: "[verb] [what it does]"

```typescript
// CORRECT — concise, actionable
description: "Follow call chains from anchor points through the substrate"
description: "Find all implementations of anchored interfaces"
description: "Expand namespace to show all contained symbols"

// WRONG — too long (>100 chars)
description: "This tool follows call chains starting from the anchor points provided by the strategizer through the substrate graph"
```

## execute() — using the substrate

```typescript
execute(request, substrate) {
  const { anchors } = request

  for (const anchor of anchors) {
    if (!anchor.node) continue

    // Query neighbors
    const neighbors = substrate.query({
      projectID:  request.ir.anchors[0]?.id ?? "",
      nodeTypes:  ["symbol"],
      limit:      request.ir.kLimit,
    })

    // Emit findings
    if (neighbors.length > 0) {
      return {
        emissions: [{
          channel: "action",
          content: `Found ${neighbors.length} related symbols`,
        }],
        proposedNodes: [],
        proposedEdges: [],
      }
    }
  }

  return { emissions: [], proposedNodes: [], proposedEdges: [] }
}
```

## Predicate naming convention

Use your plugin ID as a prefix: `com.example.my-plugin.tool-name`
Or just the tool name if it's unambiguous: `call-graph`, `interface-impl`

```yaml
# ce.yaml — user enables a tool via predicate
predicates:
  call-graph: "true"
```

## When to propose new nodes/edges

Propose new nodes when the tool discovers relationships not in the graph:
- Call graph edges from dynamic analysis
- Type inference results
- Cross-project dependency links

The engine validates proposed nodes before adding them.
Source class should be `"speculative"` unless you have high confidence.
