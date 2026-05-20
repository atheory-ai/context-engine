# Context Engine — Spec 5: Strategizer Prompt
## Implementation Spec — System Prompt, XML Schema, Tag Extractor, IR Compilation
### Version 1.0 | February 2026

---

> This is an implementation spec, not a PRD section.
> Hand this document to Claude Code alongside all prior specs.
> The prompt text here is the actual prompt. The XML schema is the contract.
> The tag extractor implementation is precise enough to implement directly.
> Companion: Context Engine PRD v0.5 Sections 9.2, 11. Decisions Log v1.0 Section 4.

---

## 1. Overview

The Strategizer is the first cognitive node. It receives a user query and
produces an IR — the compiled intent that drives the rest of the loop.

The Strategizer does not answer the question. It plans the investigation.

Its output is a set of XML tags embedded in prose. The tag extractor parses
those tags into a typed Go struct. That struct is the IR. Everything downstream
operates on the IR, not on the raw query.

---

## 2. Prompt Assembly

The Strategizer prompt is assembled dynamically at query time from static
components. Nothing in this assembly requires a substrate read — all inputs
are in memory at startup.

```go
// internal/agent/strategizer/prompt.go

package strategizer

// AssembleSystemPrompt builds the Strategizer system prompt.
// Called once per query, before the LLM call.
func AssembleSystemPrompt(
    basePrompt  string,
    archPrompt  string,
    seeds       []core.ConceptSeed,
    tools       []core.Tool,
    role        *core.RoleDefinition, // nil = default role
) string {
    var b strings.Builder

    b.WriteString(systemPromptPreamble)
    b.WriteString("\n\n")

    // Project context (always present)
    b.WriteString("## Project Context\n\n")
    b.WriteString(basePrompt)
    b.WriteString("\n\n")

    // Architectural detail (always present)
    b.WriteString("## Architecture\n\n")
    b.WriteString(archPrompt)
    b.WriteString("\n\n")

    // Concept vocabulary (from substrate, loaded at index time)
    if len(seeds) > 0 {
        b.WriteString("## Domain Vocabulary\n\n")
        b.WriteString(formatConceptSeeds(seeds))
        b.WriteString("\n\n")
    }

    // Tool registry (all available tools — built-in + plugin-contributed)
    b.WriteString("## Available Tools\n\n")
    b.WriteString(formatToolRegistry(tools))
    b.WriteString("\n\n")

    // Role context (if non-default)
    if role != nil {
        b.WriteString("## Role\n\n")
        b.WriteString(role.SystemPrompt)
        b.WriteString("\n\n")
    }

    b.WriteString(systemPromptSuffix)

    return b.String()
}
```

### formatToolRegistry — Option B Format

```go
func formatToolRegistry(tools []core.Tool) string {
    var b strings.Builder
    for _, t := range tools {
        // Name + description line
        b.WriteString(fmt.Sprintf("%-20s %s\n", t.Name(), t.Description()))
        // Activation hint (from ActivationHint() — tools implement this)
        if h, ok := t.(ToolWithHint); ok {
            b.WriteString(fmt.Sprintf("%-20s Activates when: %s\n", "", h.ActivationHint()))
        }
        b.WriteString("\n")
    }
    return b.String()
}

// ToolWithHint is an optional extension of core.Tool.
// Tools that implement it provide activation hints for the Strategizer prompt.
type ToolWithHint interface {
    ActivationHint() string
}
```

**ActivationHint implementations for built-in tools:**

| Tool | ActivationHint |
|------|----------------|
| `callgraph` | `predicate.callgraph=true, or anchors contain symbol nodes` |
| `references` | `predicate.references=true, or IR.mode=thinking with symbol anchors` |
| `crossproject` | `predicate.crossproject=true, or concept anchors span multiple namespaces` |
| `concepts` | `predicate.concepts=true, or anchors contain concept nodes` |
| `filecontext` | `predicate.filecontext=true, or anchors contain file nodes` |
| `summary` | `predicate.summary=true, or anchors contain namespace nodes` |

---

## 3. System Prompt — Full Text

This is the actual prompt. Delimiters `{{SECTION}}` are replaced at assembly time.

---

```
You are the Strategizer for a codebase intelligence engine. Your job is to
analyze a developer's query and produce a precise investigation plan.

You do NOT answer the query. You plan the investigation that will answer it.

The engine will execute your plan by activating substrate anchors, running
tools concurrently, and iterating until the open questions are resolved.
A Reviewer will evaluate each iteration. A Synthesizer will produce the
final answer from the gathered evidence.

Your output quality directly determines answer quality. Be precise.

────────────────────────────────────────────────────────────────────────────
THINKING PROCESS
────────────────────────────────────────────────────────────────────────────

Before producing your XML output, think through:

1. What is the developer actually trying to understand?
   (Not what they asked — what they need to know.)

2. What parts of the codebase are relevant?
   (Specific symbols, namespaces, concepts — not vague areas.)

3. Which tools will surface the right evidence?
   (Match tools to the structure of the question.)

4. What might be non-obvious?
   (Cross-cutting concerns, indirect relationships, shared concepts.)

5. What would leave the question unanswered?
   (Anticipate gaps — put them in open_queries.)

────────────────────────────────────────────────────────────────────────────
PROJECT CONTEXT
────────────────────────────────────────────────────────────────────────────

{{BASE_PROMPT}}

────────────────────────────────────────────────────────────────────────────
ARCHITECTURE
────────────────────────────────────────────────────────────────────────────

{{ARCH_PROMPT}}

────────────────────────────────────────────────────────────────────────────
DOMAIN VOCABULARY
────────────────────────────────────────────────────────────────────────────

These are domain-specific terms indexed from this codebase. Use them as
anchor IDs when they appear in the query or are clearly relevant.

{{CONCEPT_SEEDS}}

────────────────────────────────────────────────────────────────────────────
AVAILABLE TOOLS
────────────────────────────────────────────────────────────────────────────

Set predicates to activate the tools you want. Each tool runs concurrently
with others that activate. Tools that do not activate are not run.

{{TOOL_REGISTRY}}

────────────────────────────────────────────────────────────────────────────
OUTPUT FORMAT
────────────────────────────────────────────────────────────────────────────

After your thinking, produce exactly these XML tags. Prose between tags is
fine and expected — the extractor finds the tags regardless of surrounding text.

<mode>thinking</mode>
Use "thinking" for investigative queries requiring multiple loops.
Use "direct" for simple factual queries answerable in one pass.
Use "audit" for queries about the engine's own reasoning or history.

<anchors>
  <!-- Entry points into the substrate graph. -->
  <!-- type: symbol | namespace | concept | file -->
  <!-- id: fully-qualified canonical identifier -->
  <!-- confidence: high | medium | low -->
  <anchor type="symbol" id="pkg/path:FunctionName" confidence="high"/>
  <anchor type="namespace" id="pkg/path" confidence="high"/>
  <anchor type="concept" id="concept-term" confidence="medium"/>
</anchors>

<predicates>
  <!-- Boolean flags that activate specific tools. -->
  <!-- Set only the predicates for tools you want to run. -->
  <!-- Value must be "true" — absent predicate = tool does not activate. -->
  <predicate name="callgraph" value="true"/>
  <predicate name="references" value="true"/>
  <predicate name="crossproject" value="true"/>
  <predicate name="concepts" value="true"/>
  <predicate name="filecontext" value="true"/>
  <predicate name="summary" value="true"/>
</predicates>

<open_queries>
  <!-- Sub-questions the investigation must answer to resolve the main query. -->
  <!-- Be specific. Vague open queries produce vague answers. -->
  <!-- The Reviewer tracks these and considers the loop converged when resolved. -->
  <open_query>specific sub-question here</open_query>
</open_queries>

<max_loops>5</max_loops>
<!-- How many investigation iterations to allow. Default: 5. -->
<!-- Simple queries: 2-3. Deep architectural investigations: 6-8. -->
<!-- The loop may exit earlier if the Reviewer converges. -->

<k_limit>30</k_limit>
<!-- How many substrate nodes to surface per activation query. -->
<!-- Focused queries: 20. Broad investigations: 40-50. Default: 30. -->

<role_hint></role_hint>
<!-- Optional: name of a plugin-contributed agent role. -->
<!-- Leave empty to use default role. -->
<!-- Only set if the query clearly benefits from a specialized perspective. -->

<model_tier></model_tier>
<!-- Optional: fast | standard | thinking -->
<!-- Leave empty — the router selects the appropriate tier. -->
<!-- Set "thinking" only for queries requiring deep multi-step reasoning -->
<!-- where you are uncertain about the investigation structure. -->

────────────────────────────────────────────────────────────────────────────
ANCHOR ID CONVENTIONS
────────────────────────────────────────────────────────────────────────────

symbol:    "package/path/from/root:SymbolName"
           Examples: "internal/billing:ProcessPayment"
                     "cmd/ce/main:main"
                     "internal/graph/substrate:ReadWriter.UpsertNode"

namespace: "package/path/from/root"
           Examples: "internal/billing"
                     "internal/graph"

concept:   "lowercase-hyphenated-term"
           Examples: "billing-event", "volunteer-op", "co-activation"
           Must match a term from the Domain Vocabulary section above.

file:      "relative/path/from/root.ext"
           Examples: "internal/billing/invoice.go"
                     "internal/graph/substrate/writer.go"

When uncertain about a canonical ID, use medium or low confidence.
The activation layer handles unresolved anchors gracefully.

────────────────────────────────────────────────────────────────────────────
RULES
────────────────────────────────────────────────────────────────────────────

1. Always produce at least one anchor. An investigation with no entry point
   cannot begin.

2. Always produce at least one open_query. An investigation with no questions
   has nothing to resolve.

3. Do not produce anchors you are not confident exist. Low-confidence anchors
   are fine — fabricated anchors waste activation budget.

4. Set predicates sparingly. Activating every tool on every query produces
   noise. Activate the tools that match the structure of the question.

5. The callgraph predicate is the most expensive tool. Set it only when
   the query is explicitly about execution flow, call chains, or data flow.

6. Concept anchors expand laterally. Use them when the query involves
   understanding a domain concept rather than a specific symbol.

7. If the query is ambiguous, prefer more open_queries over more anchors.
   The loop can refine. A wrong anchor wastes an iteration.

8. max_loops and k_limit affect cost and latency. Be conservative by default.
   Increase only when you can justify why more iterations or nodes are needed.
```

---

## 4. XML Tag Schema

Every tag the extractor looks for, precisely defined.

```
Tag             Required  Repeatable  Attributes
──────────────  ────────  ──────────  ──────────────────────────────────────
<mode>          yes       no          none — content: thinking|direct|audit
<anchors>       yes       no          none — container
  <anchor>      yes*      yes         type, id, confidence
                                      * at least one anchor required
<predicates>    no        no          none — container
  <predicate>   no        yes         name, value
<open_queries>  yes       no          none — container
  <open_query>  yes*      yes         none — content is the question text
                                      * at least one required
<max_loops>     no        no          none — content: integer 1-20
<k_limit>       no        no          none — content: integer 10-100
<role_hint>     no        no          none — content: role name or empty
<model_tier>    no        no          none — content: fast|standard|thinking|empty
```

### Attribute values

```
anchor.type:        symbol | namespace | concept | file
anchor.confidence:  high | medium | low
predicate.name:     callgraph | references | crossproject |
                    concepts | filecontext | summary |
                    <plugin-defined-predicate>
predicate.value:    true (only value ever used — absent = false)
```

---

## 5. Tag Extractor

The extractor is **not** a full XML parser. It searches for known tags by
name, extracts their content, and parses attributes with a simple scanner.
It does not validate XML structure, handle namespaces, or support nested
unknown tags. This is intentional — LLM output is not valid XML, and a
lenient extractor is more reliable than a strict one.

```go
// internal/agent/strategizer/extractor.go

package strategizer

import (
    "fmt"
    "regexp"
    "strconv"
    "strings"

    "github.com/atheory-ai/context-engine/internal/core"
)

// Extract parses the Strategizer's response text and returns an IR.
// Returns an error only if required tags are absent or have invalid values.
// Missing optional tags use defaults. Extra text between tags is ignored.
func Extract(response string) (*core.IR, error) {
    ir := &core.IR{}

    // ── mode ──────────────────────────────────────────────────────────────
    mode, err := extractText(response, "mode")
    if err != nil {
        // mode is required — default to thinking if absent
        ir.Mode = core.IRModeThinking
    } else {
        switch strings.TrimSpace(mode) {
        case "thinking":
            ir.Mode = core.IRModeThinking
        case "direct":
            ir.Mode = core.IRModeDirect
        case "audit":
            ir.Mode = core.IRModeAudit
        default:
            ir.Mode = core.IRModeThinking // unknown value → thinking
        }
    }

    // ── anchors ───────────────────────────────────────────────────────────
    ir.Anchors = extractAnchors(response)

    // ── predicates ────────────────────────────────────────────────────────
    ir.Predicates = extractPredicates(response)

    // ── open_queries ──────────────────────────────────────────────────────
    ir.OpenQueries = extractOpenQueries(response)

    // ── max_loops ─────────────────────────────────────────────────────────
    if v, err := extractInt(response, "max_loops", 1, 20); err == nil {
        ir.MaxLoops = v
    }
    // 0 = use project default (resolved in runner)

    // ── k_limit ───────────────────────────────────────────────────────────
    if v, err := extractInt(response, "k_limit", 10, 100); err == nil {
        ir.KLimit = v
    }

    // ── role_hint ─────────────────────────────────────────────────────────
    if v, err := extractText(response, "role_hint"); err == nil {
        ir.RoleHint = strings.TrimSpace(v)
    }

    // ── model_tier ────────────────────────────────────────────────────────
    if v, err := extractText(response, "model_tier"); err == nil {
        tier := strings.TrimSpace(v)
        switch tier {
        case "fast", "standard", "thinking":
            ir.ModelTier = tier
        }
    }

    return ir, nil
}

// ── Extraction helpers ─────────────────────────────────────────────────────

// extractText finds the content of a simple tag: <tag>content</tag>
// Returns error if tag not found.
func extractText(s, tag string) (string, error) {
    open := "<" + tag + ">"
    close := "</" + tag + ">"
    start := strings.Index(s, open)
    if start == -1 {
        return "", fmt.Errorf("tag <%s> not found", tag)
    }
    start += len(open)
    end := strings.Index(s[start:], close)
    if end == -1 {
        return "", fmt.Errorf("tag </%s> not found", tag)
    }
    return s[start : start+end], nil
}

// extractInt finds a tag with integer content, clamped to [min, max].
func extractInt(s, tag string, min, max int) (int, error) {
    v, err := extractText(s, tag)
    if err != nil {
        return 0, err
    }
    n, err := strconv.Atoi(strings.TrimSpace(v))
    if err != nil {
        return 0, fmt.Errorf("tag <%s> not an integer: %s", tag, v)
    }
    if n < min {
        n = min
    }
    if n > max {
        n = max
    }
    return n, nil
}

// anchorRegex matches self-closing and open/close anchor tags.
// Handles attribute order variations and extra whitespace.
var anchorRegex = regexp.MustCompile(
    `<anchor\s+([^>]*?)(?:/>|>.*?</anchor>)`,
)

var attrRegex = regexp.MustCompile(`(\w+)="([^"]*)"`)

func extractAnchors(s string) []core.AnchorRef {
    var anchors []core.AnchorRef
    matches := anchorRegex.FindAllStringSubmatch(s, -1)
    for _, m := range matches {
        attrs := parseAttrs(m[1])
        anchorType, ok1 := attrs["type"]
        anchorID, ok2 := attrs["id"]
        if !ok1 || !ok2 || anchorID == "" {
            continue
        }
        confidence := attrs["confidence"]
        if confidence == "" {
            confidence = "medium"
        }
        // Validate type
        switch anchorType {
        case "symbol", "namespace", "concept", "file":
            // valid
        default:
            continue // skip unknown types
        }
        anchors = append(anchors, core.AnchorRef{
            Type:       anchorType,
            ID:         anchorID,
            Confidence: confidence,
        })
    }
    return anchors
}

func extractPredicates(s string) map[string]string {
    predicates := make(map[string]string)

    predRegex := regexp.MustCompile(`<predicate\s+([^>]*?)(?:/>|>.*?</predicate>)`)
    matches := predRegex.FindAllStringSubmatch(s, -1)
    for _, m := range matches {
        attrs := parseAttrs(m[1])
        name, ok1 := attrs["name"]
        value, ok2 := attrs["value"]
        if !ok1 || !ok2 || name == "" {
            continue
        }
        predicates[name] = value
    }
    return predicates
}

func extractOpenQueries(s string) []string {
    var queries []string

    // Find the <open_queries> container first
    container, err := extractText(s, "open_queries")
    if err != nil {
        return queries
    }

    // Extract individual <open_query> tags within the container
    oqRegex := regexp.MustCompile(`<open_query>(.*?)</open_query>`)
    matches := oqRegex.FindAllStringSubmatch(container, -1)
    for _, m := range matches {
        q := strings.TrimSpace(m[1])
        if q != "" {
            queries = append(queries, q)
        }
    }
    return queries
}

func parseAttrs(attrStr string) map[string]string {
    attrs := make(map[string]string)
    matches := attrRegex.FindAllStringSubmatch(attrStr, -1)
    for _, m := range matches {
        attrs[m[1]] = m[2]
    }
    return attrs
}
```

---

## 6. IR Validation

Validation runs immediately after extraction. It catches structural problems
before the IR enters the activation layer.

```go
// internal/core/ir.go (Validate implementation)

func (ir *IR) Validate() error {
    // Required: at least one anchor
    if len(ir.Anchors) == 0 {
        return fmt.Errorf("%w: no anchors — investigation has no entry point", ErrInvalidIR)
    }

    // Required: at least one open query
    if len(ir.OpenQueries) == 0 {
        return fmt.Errorf("%w: no open_queries — investigation has nothing to resolve", ErrInvalidIR)
    }

    // Anchor validation
    for i, a := range ir.Anchors {
        if a.ID == "" {
            return fmt.Errorf("%w: anchor[%d] has empty ID", ErrInvalidIR, i)
        }
        switch a.Type {
        case "symbol", "namespace", "concept", "file":
            // valid
        default:
            return fmt.Errorf("%w: anchor[%d] has unknown type %q", ErrInvalidIR, i, a.Type)
        }
        switch a.Confidence {
        case "high", "medium", "low":
            // valid
        default:
            ir.Anchors[i].Confidence = "medium" // coerce rather than reject
        }
    }

    // Predicate value validation
    for name, value := range ir.Predicates {
        if value != "true" {
            // Only "true" is meaningful — drop non-true predicates
            delete(ir.Predicates, name)
        }
    }

    // MaxLoops range
    if ir.MaxLoops < 0 || ir.MaxLoops > 20 {
        ir.MaxLoops = 0 // coerce to default
    }

    // KLimit range
    if ir.KLimit < 0 || ir.KLimit > 100 {
        ir.KLimit = 0 // coerce to default
    }

    return nil
}
```

### Validation philosophy

- **Hard errors** (return error): no anchors, no open queries. These make the
  loop impossible to run. The Strategizer must be retried.
- **Soft corrections** (coerce silently): bad confidence value, out-of-range
  numbers, non-true predicate values. These are recoverable without a retry.
- **Retry budget**: The Strategizer is allowed one retry on hard error.
  If the second attempt also fails validation, the query returns an error
  to the user with the validation failure reason.

```go
// internal/agent/strategizer/strategizer.go

func (n *Node) Run(rc *runner.RunContext) (*core.IR, error) {
    for attempt := 0; attempt < 2; attempt++ {
        resp, err := n.llm.Complete(rc.Ctx, n.buildRequest(rc))
        if err != nil {
            return nil, fmt.Errorf("strategizer LLM: %w", err)
        }

        rc.Budget.Record(resp.TokensIn, resp.TokensOut, estimateCost(resp))
        n.logCall(rc, resp) // write to execution log

        ir, err := Extract(resp.Content)
        if err != nil {
            // Extraction error — retry
            rc.Ch.Emit(core.Emission{
                Channel: core.ChanDebug,
                Content: fmt.Sprintf("strategizer extraction error (attempt %d): %v", attempt+1, err),
            })
            continue
        }

        if err := ir.Validate(); err != nil {
            if attempt == 0 {
                // First failure — retry with the validation error in context
                rc.Ch.Emit(core.Emission{
                    Channel: core.ChanDebug,
                    Content: fmt.Sprintf("strategizer IR invalid (attempt 1): %v — retrying", err),
                })
                n.lastValidationError = err.Error()
                continue
            }
            // Second failure — give up
            return nil, fmt.Errorf("strategizer produced invalid IR after 2 attempts: %w", err)
        }

        // Emit the IR to the thinking channel for CE Studio
        irJSON, _ := json.Marshal(ir)
        rc.Ch.Emit(core.Emission{
            RunID:   rc.RunID,
            TurnID:  rc.TurnID,
            Channel: core.ChanThinking,
            Content: fmt.Sprintf("IR compiled: %s", string(irJSON)),
            Metadata: map[string]any{"ir": ir},
        })

        return ir, nil
    }
    return nil, fmt.Errorf("strategizer failed after 2 attempts")
}
```

### Retry prompt amendment

On validation failure, the second attempt appends to the user message:

```go
func (n *Node) buildRequest(rc *runner.RunContext) core.CompletionRequest {
    userContent := rc.Query
    if n.lastValidationError != "" {
        userContent += fmt.Sprintf(
            "\n\n[Previous attempt failed validation: %s. "+
            "Ensure your output includes at least one <anchor> and at least one <open_query>.]",
            n.lastValidationError,
        )
    }
    // ...
}
```

---

## 7. Concept Seed Formatting

Concept seeds are formatted as a compact vocabulary table injected into the
Domain Vocabulary section:

```go
func formatConceptSeeds(seeds []core.ConceptSeed) string {
    if len(seeds) == 0 {
        return "(no domain vocabulary indexed yet)"
    }
    var b strings.Builder
    for _, s := range seeds {
        b.WriteString(fmt.Sprintf("%-30s %s\n", s.Term, s.Definition))
        if len(s.Related) > 0 {
            b.WriteString(fmt.Sprintf("%-30s related: %s\n", "",
                strings.Join(s.Related, ", ")))
        }
    }
    return b.String()
}
```

Example rendered output:

```
billing-event                  A financial transaction record in the billing subsystem
                               related: invoice, payment, charge, refund
volunteer-op                   An operation record for volunteer assignment
                               related: assignment, scheduling, vol-status
co-activation                  Two substrate nodes activated in the same cognitive loop
                               related: hebbian-weight, associative-edge
```

---

## 8. Test Cases

These are the canonical test cases for the extractor and IR validator.
Implement as `extractor_test.go` in the `strategizer` package.

### Test Case 1 — Standard investigative query

**Input query**: `"How does volunteer assignment affect billing event generation?"`

**Expected Strategizer response** (the LLM produces something like this):

```
Let me analyze this query. It asks about the relationship between volunteer
assignment operations and billing event creation — this is a cross-cutting
concern involving at least two subsystems.

<mode>thinking</mode>

The key entry points are the assignment operation and the billing event type.
I need to understand both the call chain from assignment to billing, and whether
there's a direct relationship or whether it's mediated by another mechanism.

<anchors>
  <anchor type="concept" id="volunteer-op" confidence="high"/>
  <anchor type="concept" id="billing-event" confidence="high"/>
  <anchor type="namespace" id="internal/volunteer" confidence="medium"/>
  <anchor type="namespace" id="internal/billing" confidence="medium"/>
</anchors>

<predicates>
  <predicate name="callgraph" value="true"/>
  <predicate name="concepts" value="true"/>
  <predicate name="crossproject" value="false"/>
</predicates>

<open_queries>
  <open_query>What function or method in the volunteer subsystem triggers billing event creation?</open_query>
  <open_query>Is billing triggered directly from assignment, or through an event/message bus?</open_query>
  <open_query>What fields on a volunteer-op determine the billing event type or amount?</open_query>
</open_queries>

<max_loops>4</max_loops>
<k_limit>30</k_limit>
<role_hint></role_hint>
<model_tier></model_tier>
```

**Expected IR**:

```go
&core.IR{
    Mode: core.IRModeThinking,
    Anchors: []core.AnchorRef{
        {Type: "concept",   ID: "volunteer-op",       Confidence: "high"},
        {Type: "concept",   ID: "billing-event",      Confidence: "high"},
        {Type: "namespace", ID: "internal/volunteer",  Confidence: "medium"},
        {Type: "namespace", ID: "internal/billing",    Confidence: "medium"},
    },
    Predicates: map[string]string{
        "callgraph": "true",
        "concepts":  "true",
        // "crossproject": "false" is dropped — only "true" values are kept
    },
    OpenQueries: []string{
        "What function or method in the volunteer subsystem triggers billing event creation?",
        "Is billing triggered directly from assignment, or through an event/message bus?",
        "What fields on a volunteer-op determine the billing event type or amount?",
    },
    MaxLoops: 4,
    KLimit:   30,
}
```

**Validation**: passes. ✓

---

### Test Case 2 — Simple direct query

**Input query**: `"What does ProcessPayment return?"`

**Expected response**:

```
This is a direct question about a specific symbol's return type.
No deep investigation needed.

<mode>direct</mode>

<anchors>
  <anchor type="symbol" id="internal/billing:ProcessPayment" confidence="high"/>
</anchors>

<predicates>
  <predicate name="references" value="true"/>
</predicates>

<open_queries>
  <open_query>What are the return types of ProcessPayment?</open_query>
</open_queries>

<max_loops>2</max_loops>
<k_limit>20</k_limit>
<role_hint></role_hint>
<model_tier></model_tier>
```

**Expected IR**:

```go
&core.IR{
    Mode: core.IRModeDirect,
    Anchors: []core.AnchorRef{
        {Type: "symbol", ID: "internal/billing:ProcessPayment", Confidence: "high"},
    },
    Predicates: map[string]string{
        "references": "true",
    },
    OpenQueries: []string{
        "What are the return types of ProcessPayment?",
    },
    MaxLoops: 2,
    KLimit:   20,
}
```

**Validation**: passes. ✓

---

### Test Case 3 — IR validation failure (no anchors)

**Input response** (malformed LLM output):

```
<mode>thinking</mode>
<anchors>
</anchors>
<open_queries>
  <open_query>How does billing work?</open_query>
</open_queries>
```

**Expected IR after extraction**:

```go
&core.IR{
    Mode:        core.IRModeThinking,
    Anchors:     []core.AnchorRef{},  // empty
    OpenQueries: []string{"How does billing work?"},
}
```

**Validation**: fails with `ErrInvalidIR: no anchors — investigation has no entry point`. ✓

Triggers retry. Second attempt appended with validation error context.

---

### Test Case 4 — Malformed XML, extractor recovers

**Input response** (missing closing tags, attributes out of order):

```
Thinking about this...

<mode>thinking

<anchors>
  <anchor id="internal/graph:SubstrateReader" type="symbol" confidence=high/>
  <anchor type="namespace" id="internal/graph" confidence="medium">
</anchors>

<open_queries>
  <open_query>What does SubstrateReader expose?</open_query>
</open_queries>
```

**Expected IR** (extractor recovers what it can):

```go
&core.IR{
    Mode: core.IRModeThinking,  // extractText finds "thinking\n" after <mode>, trims it
    Anchors: []core.AnchorRef{
        // First anchor: attrRegex finds id and type despite order, but confidence="high"
        // has no quotes — attrRegex requires quotes, so confidence defaults to "medium"
        {Type: "symbol", ID: "internal/graph:SubstrateReader", Confidence: "medium"},
        // Second anchor: parsed correctly despite missing closing />
        {Type: "namespace", ID: "internal/graph", Confidence: "medium"},
    },
    OpenQueries: []string{"What does SubstrateReader expose?"},
}
```

**Validation**: passes (anchors present, open query present). ✓

Note: `confidence=high` without quotes is not matched by `attrRegex` (requires `"..."`)
and defaults to `"medium"`. This is correct behavior — lenient extractor, safe default.

---

### Test Case 5 — Plugin predicate

A plugin-contributed tool registers predicate `"go-test-runner"`.

**Input response**:

```
<mode>thinking</mode>
<anchors>
  <anchor type="namespace" id="internal/billing" confidence="high"/>
</anchors>
<predicates>
  <predicate name="callgraph" value="true"/>
  <predicate name="go-test-runner" value="true"/>
</predicates>
<open_queries>
  <open_query>Which tests cover the billing subsystem?</open_query>
</open_queries>
```

**Expected IR**:

```go
&core.IR{
    Predicates: map[string]string{
        "callgraph":      "true",
        "go-test-runner": "true",  // plugin predicate — stored as-is
    },
    // ...
}
```

**Validation**: passes. The validator does not enforce predicate names —
unknown predicate names are valid. The tool registry determines which
predicates activate which tools.

---

## 9. Strategizer Node Package Layout

```
internal/agent/strategizer/
  strategizer.go    — Node struct, Run(), retry logic
  prompt.go         — AssembleSystemPrompt(), formatToolRegistry(),
                      formatConceptSeeds()
  extractor.go      — Extract(), all extractX() helpers
  extractor_test.go — All 5 test cases above + edge cases
```

---

## 10. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| Strategizer context | base_prompt + arch_prompt + concept_seeds + tool_registry |
| Tool format in prompt | Option B: name + description + activation hint |
| Substrate injection | None — tools surface live substrate state during the loop |
| Output format | Pseudo-XML tags in prose — extractor finds tags, ignores rest |
| Extractor type | Targeted (regex-based), not a full XML parser |
| Required tags | mode, at least one anchor, at least one open_query |
| Optional tags | predicates, max_loops, k_limit, role_hint, model_tier |
| Validation hard errors | No anchors, no open queries |
| Validation soft corrections | Bad confidence, out-of-range numbers, non-true predicates |
| Retry budget | 1 retry on hard validation error — 2 attempts total |
| Retry amendment | Validation error appended to user message on second attempt |
| Plugin predicates | Accepted — validator does not enforce predicate names |
| Strategizer LLM tier | Standard (router default — can be overridden by model_tier tag) |

---

*Spec 5: Strategizer Prompt — v1.0 — February 2026*
*Next: Spec 6 — CLI / Config*
*Companion: Context Engine PRD v0.5 Sections 9.2, 11 | Decisions Log v1.0 Section 4*
