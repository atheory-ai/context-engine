# Context Engine — Spec 12: LLM Provider, Router, Reviewer & Synthesizer Prompts
## Implementation Spec — Anthropic Provider, Tier Routing, Prompt Text
### Version 1.0 | February 2026

---

> This spec covers the LLM layer and the two remaining agent prompts.
> Hand to Claude Code alongside spec-2-packages.md and spec-3-engine-runner.md.
> Companion: Context Engine PRD v0.5 Sections 9.3, 9.4, 11. Decisions Log v1.0 Section 7.

---

## 1. Overview

This spec covers three things:

1. **LLM Provider** — the Anthropic API implementation, streaming, extended
   thinking, retry/backoff, cost estimation
2. **LLM Router** — tier selection (fast/standard/thinking), model mapping,
   budget awareness
3. **Reviewer & Synthesizer prompts** — the actual prompt text for the two
   remaining agent nodes (Strategizer was Spec 5)

---

## 2. Package Structure

```
internal/llm/
  router.go           — Router struct, tier selection, model mapping
  types.go            — CompletionRequest, CompletionResponse, ModelInfo
  budget.go           — cost estimation per model
  anthropic/
    provider.go       — AnthropicProvider, Complete(), Stream()
    retry.go          — retry/backoff logic
    thinking.go       — extended thinking support
    models.go         — model IDs, context limits, pricing
  openai/
    provider.go       — OpenAIProvider (stub for Phase 2, full in Phase 3)
  local/
    provider.go       — LocalProvider via Ollama (stub for Phase 2)
```

---

## 3. Core LLM Types

```go
// internal/llm/types.go

package llm

// CompletionRequest is sent to any LLM provider.
type CompletionRequest struct {
    Model    string
    System   string
    Messages []Message
    MaxTokens int         // 0 = use model default
    Thinking  *ThinkingConfig // nil = no extended thinking
    Stream    bool
}

type Message struct {
    Role    string // "user" | "assistant"
    Content string
}

// ThinkingConfig enables Claude's extended thinking.
type ThinkingConfig struct {
    BudgetTokens int // how many tokens to allocate to thinking
}

// CompletionResponse is returned from any LLM provider.
type CompletionResponse struct {
    Content      string
    ThinkingText string  // populated when extended thinking was used
    Model        string
    TokensIn     int
    TokensOut    int
    ThinkingTokens int   // thinking tokens (billed separately)
    StopReason   string  // "end_turn" | "max_tokens" | "stop_sequence"
    LatencyMS    int64
}

// ModelInfo describes a model's capabilities and pricing.
type ModelInfo struct {
    ID             string
    ContextLimit   int     // max tokens in context window
    MaxOutputTokens int
    InputPricePer1M  float64  // USD per 1M input tokens
    OutputPricePer1M float64  // USD per 1M output tokens
    ThinkingPricePer1M float64 // USD per 1M thinking tokens (0 if not supported)
    SupportsThinking bool
}
```

---

## 4. Anthropic Models

```go
// internal/llm/anthropic/models.go

package anthropic

var Models = map[string]llm.ModelInfo{
    "claude-haiku-4-5-20251001": {
        ID:               "claude-haiku-4-5-20251001",
        ContextLimit:     200000,
        MaxOutputTokens:  8192,
        InputPricePer1M:  0.80,
        OutputPricePer1M: 4.00,
        SupportsThinking: false,
    },
    "claude-sonnet-4-6": {
        ID:               "claude-sonnet-4-6",
        ContextLimit:     200000,
        MaxOutputTokens:  16000,
        InputPricePer1M:  3.00,
        OutputPricePer1M: 15.00,
        SupportsThinking: false,
    },
    "claude-opus-4-6": {
        ID:               "claude-opus-4-6",
        ContextLimit:     200000,
        MaxOutputTokens:  32000,
        InputPricePer1M:  15.00,
        OutputPricePer1M: 75.00,
        ThinkingPricePer1M: 75.00,
        SupportsThinking: true,
    },
}

// DefaultModels maps tiers to model IDs.
// Overridden by ce.yaml llm.models section.
var DefaultModels = map[string]string{
    "fast":     "claude-haiku-4-5-20251001",
    "standard": "claude-sonnet-4-6",
    "thinking": "claude-opus-4-6",
}
```

---

## 5. LLM Router

```go
// internal/llm/router.go

package llm

// Router selects the appropriate model for each request
// and delegates to the configured provider.
type Router struct {
    provider  Provider
    models    map[string]string // tier → model ID
    modelInfo map[string]ModelInfo
}

// Provider is the interface all LLM providers implement.
type Provider interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
    ModelInfo(modelID string) (ModelInfo, bool)
    Name() string
}

func NewRouter(cfg config.LLMConfig) (*Router, error) {
    provider, err := buildProvider(cfg)
    if err != nil {
        return nil, err
    }

    models := cfg.Models
    if len(models) == 0 {
        models = anthropic.DefaultModels
    }

    return &Router{
        provider:  provider,
        models:    models,
        modelInfo: buildModelInfoMap(provider),
    }, nil
}

// Complete routes a completion request through the appropriate model.
func (r *Router) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
    // If model is already set (e.g., by Strategizer model_tier override), use it
    if req.Model == "" {
        req.Model = r.models["standard"]
    }
    return r.provider.Complete(ctx, req)
}

// ModelForTier returns the model ID for a given tier.
func (r *Router) ModelForTier(tier string) string {
    if model, ok := r.models[tier]; ok {
        return model
    }
    return r.models["standard"]
}

// ModelInfo returns capability and pricing info for a model.
func (r *Router) ModelInfo() ModelInfo {
    modelID := r.models["standard"]
    if info, ok := r.provider.ModelInfo(modelID); ok {
        return info
    }
    // Safe default for budget initialization
    return ModelInfo{ContextLimit: 200000}
}

// ContextLimit returns the context limit for the current standard model.
// Used by Budget initialization.
func (r *Router) ContextLimit() int {
    return r.ModelInfo().ContextLimit
}

func buildProvider(cfg config.LLMConfig) (Provider, error) {
    switch cfg.Provider {
    case "anthropic":
        apiKey := cfg.APIKey
        if apiKey == "" {
            apiKey = os.Getenv("ANTHROPIC_API_KEY")
        }
        if apiKey == "" {
            return nil, fmt.Errorf(
                "Anthropic API key not configured. " +
                "Set ANTHROPIC_API_KEY environment variable or llm.api_key in ce.yaml")
        }
        return anthropic.NewProvider(apiKey, cfg.BaseURL, cfg.TimeoutSeconds), nil

    case "openai":
        apiKey := cfg.APIKey
        if apiKey == "" {
            apiKey = os.Getenv("OPENAI_API_KEY")
        }
        return openai.NewProvider(apiKey, cfg.BaseURL, cfg.TimeoutSeconds), nil

    case "local":
        baseURL := cfg.BaseURL
        if baseURL == "" {
            baseURL = "http://localhost:11434" // Ollama default
        }
        return local.NewProvider(baseURL), nil

    default:
        return nil, fmt.Errorf("unknown LLM provider: %q", cfg.Provider)
    }
}
```

---

## 6. Anthropic Provider

```go
// internal/llm/anthropic/provider.go

package anthropic

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "net/http"
    "time"
)

const (
    DefaultBaseURL    = "https://api.anthropic.com"
    APIVersion        = "2023-06-01"
    BetaHeader        = "interleaved-thinking-2025-05-14"
    MessagesEndpoint  = "/v1/messages"
)

type Provider struct {
    apiKey     string
    baseURL    string
    httpClient *http.Client
    retrier    *Retrier
}

func NewProvider(apiKey, baseURL string, timeoutSeconds int) *Provider {
    if baseURL == "" {
        baseURL = DefaultBaseURL
    }
    return &Provider{
        apiKey:  apiKey,
        baseURL: baseURL,
        httpClient: &http.Client{
            Timeout: time.Duration(timeoutSeconds) * time.Second,
        },
        retrier: NewRetrier(3), // 3 retries with exponential backoff
    }
}

func (p *Provider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.CompletionResponse, error) {
    start := time.Now()

    body, err := p.buildRequestBody(req)
    if err != nil {
        return llm.CompletionResponse{}, fmt.Errorf("build request: %w", err)
    }

    var apiResp anthropicResponse
    err = p.retrier.Do(ctx, func() error {
        return p.doRequest(ctx, body, &apiResp)
    })
    if err != nil {
        return llm.CompletionResponse{}, err
    }

    return p.parseResponse(apiResp, req.Model, time.Since(start)), nil
}

// ── Request building ───────────────────────────────────────────────────────

type anthropicRequest struct {
    Model     string              `json:"model"`
    MaxTokens int                 `json:"max_tokens"`
    System    string              `json:"system,omitempty"`
    Messages  []anthropicMessage  `json:"messages"`
    Thinking  *anthropicThinking  `json:"thinking,omitempty"`
    Betas     []string            `json:"betas,omitempty"`
}

type anthropicMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type anthropicThinking struct {
    Type         string `json:"type"`          // "enabled"
    BudgetTokens int    `json:"budget_tokens"`
}

func (p *Provider) buildRequestBody(req llm.CompletionRequest) ([]byte, error) {
    maxTokens := req.MaxTokens
    if maxTokens == 0 {
        info, ok := Models[req.Model]
        if ok {
            maxTokens = info.MaxOutputTokens
        } else {
            maxTokens = 8192 // safe default
        }
    }

    apiReq := anthropicRequest{
        Model:     req.Model,
        MaxTokens: maxTokens,
        System:    req.System,
    }

    for _, msg := range req.Messages {
        apiReq.Messages = append(apiReq.Messages, anthropicMessage{
            Role:    msg.Role,
            Content: msg.Content,
        })
    }

    // Extended thinking
    if req.Thinking != nil {
        apiReq.Thinking = &anthropicThinking{
            Type:         "enabled",
            BudgetTokens: req.Thinking.BudgetTokens,
        }
        apiReq.Betas = []string{BetaHeader}
    }

    return json.Marshal(apiReq)
}

// ── HTTP execution ─────────────────────────────────────────────────────────

func (p *Provider) doRequest(ctx context.Context, body []byte, out *anthropicResponse) error {
    httpReq, err := http.NewRequestWithContext(ctx, "POST",
        p.baseURL+MessagesEndpoint, bytes.NewReader(body))
    if err != nil {
        return err
    }

    httpReq.Header.Set("Content-Type", "application/json")
    httpReq.Header.Set("x-api-key", p.apiKey)
    httpReq.Header.Set("anthropic-version", APIVersion)

    resp, err := p.httpClient.Do(httpReq)
    if err != nil {
        return fmt.Errorf("http: %w", err)
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        var errBody anthropicError
        json.NewDecoder(resp.Body).Decode(&errBody)
        return &APIError{
            StatusCode: resp.StatusCode,
            Type:       errBody.Error.Type,
            Message:    errBody.Error.Message,
        }
    }

    return json.NewDecoder(resp.Body).Decode(out)
}

// ── Response parsing ───────────────────────────────────────────────────────

type anthropicResponse struct {
    ID           string              `json:"id"`
    Model        string              `json:"model"`
    Content      []anthropicContent  `json:"content"`
    Usage        anthropicUsage      `json:"usage"`
    StopReason   string              `json:"stop_reason"`
}

type anthropicContent struct {
    Type       string `json:"type"`    // "text" | "thinking"
    Text       string `json:"text"`    // for type="text"
    Thinking   string `json:"thinking"` // for type="thinking"
}

type anthropicUsage struct {
    InputTokens        int `json:"input_tokens"`
    OutputTokens       int `json:"output_tokens"`
    CacheReadTokens    int `json:"cache_read_input_tokens"`
    CacheCreateTokens  int `json:"cache_creation_input_tokens"`
}

type anthropicError struct {
    Error struct {
        Type    string `json:"type"`
        Message string `json:"message"`
    } `json:"error"`
}

func (p *Provider) parseResponse(
    apiResp anthropicResponse,
    model   string,
    latency time.Duration,
) llm.CompletionResponse {
    var textContent, thinkingContent string

    for _, block := range apiResp.Content {
        switch block.Type {
        case "text":
            textContent += block.Text
        case "thinking":
            thinkingContent += block.Thinking
        }
    }

    info, _ := Models[model]

    return llm.CompletionResponse{
        Content:      textContent,
        ThinkingText: thinkingContent,
        Model:        model,
        TokensIn:     apiResp.Usage.InputTokens,
        TokensOut:    apiResp.Usage.OutputTokens,
        StopReason:   apiResp.StopReason,
        LatencyMS:    latency.Milliseconds(),
        // Cost is computed by the Budget tracker, not here
    }
}

func (p *Provider) ModelInfo(modelID string) (llm.ModelInfo, bool) {
    info, ok := Models[modelID]
    return info, ok
}

func (p *Provider) Name() string { return "anthropic" }
```

---

## 7. Retry Logic

```go
// internal/llm/anthropic/retry.go

package anthropic

// APIError is a structured error from the Anthropic API.
type APIError struct {
    StatusCode int
    Type       string
    Message    string
}

func (e *APIError) Error() string {
    return fmt.Sprintf("anthropic API %d (%s): %s", e.StatusCode, e.Type, e.Message)
}

// IsRetryable returns true for errors that should be retried.
func (e *APIError) IsRetryable() bool {
    switch e.StatusCode {
    case 429: // rate limit
        return true
    case 500, 502, 503, 529: // server errors / overloaded
        return true
    default:
        return false
    }
}

type Retrier struct {
    maxRetries int
}

func NewRetrier(maxRetries int) *Retrier {
    return &Retrier{maxRetries: maxRetries}
}

// Do executes fn with exponential backoff retry.
func (r *Retrier) Do(ctx context.Context, fn func() error) error {
    var lastErr error

    for attempt := 0; attempt <= r.maxRetries; attempt++ {
        if attempt > 0 {
            // Exponential backoff: 1s, 2s, 4s
            backoff := time.Duration(1<<uint(attempt-1)) * time.Second
            // Add jitter: ±20%
            jitter := time.Duration(rand.Int63n(int64(backoff / 5)))
            delay := backoff + jitter

            select {
            case <-time.After(delay):
            case <-ctx.Done():
                return ctx.Err()
            }
        }

        err := fn()
        if err == nil {
            return nil
        }

        lastErr = err

        var apiErr *APIError
        if errors.As(err, &apiErr) && !apiErr.IsRetryable() {
            return err // non-retryable — fail immediately
        }

        // Context error — don't retry
        if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
            return err
        }
    }

    return fmt.Errorf("after %d retries: %w", r.maxRetries, lastErr)
}
```

---

## 8. Cost Estimation

```go
// internal/llm/budget.go (cost estimation addition)

// EstimateCost computes the estimated USD cost for a completion response.
func EstimateCost(resp llm.CompletionResponse, provider llm.Provider) float64 {
    info, ok := provider.ModelInfo(resp.Model)
    if !ok {
        return 0
    }

    inputCost  := float64(resp.TokensIn)  / 1_000_000 * info.InputPricePer1M
    outputCost := float64(resp.TokensOut) / 1_000_000 * info.OutputPricePer1M
    thinkingCost := 0.0
    if resp.ThinkingTokens > 0 {
        thinkingCost = float64(resp.ThinkingTokens) / 1_000_000 * info.ThinkingPricePer1M
    }

    return inputCost + outputCost + thinkingCost
}
```

---

## 9. Extended Thinking

Extended thinking is used only for the Strategizer when the model tier is
"thinking" (claude-opus-4-6). The thinking trace is:

- Written to the execution log (for CE Studio to display)
- Emitted to ChanThinking (visible in TUI debug mode)
- NOT included in subsequent context (Anthropic strips it)

```go
// internal/agent/strategizer/strategizer.go (thinking config)

func (n *Node) buildRequest(rc *runner.RunContext) llm.CompletionRequest {
    model := n.llm.ModelForTier(core.TierStandard)

    // Use thinking tier if Strategizer deems query complex
    // OR if the IR from a previous (failed) attempt requested it
    if n.shouldUseThinking(rc) {
        model = n.llm.ModelForTier(core.TierThinking)
    }

    req := llm.CompletionRequest{
        Model:    model,
        System:   n.assembledSystemPrompt,
        Messages: []llm.Message{{Role: "user", Content: rc.Query}},
    }

    // Enable extended thinking for thinking-tier model
    info, ok := n.llm.ModelInfo(model)
    if ok && info.SupportsThinking {
        req.Thinking = &llm.ThinkingConfig{
            BudgetTokens: 8000, // ~8K tokens for strategic planning
        }
    }

    return req
}

func (n *Node) shouldUseThinking(rc *runner.RunContext) bool {
    // Use thinking tier if:
    // 1. The user forced it via --model thinking flag
    // 2. The IR from retry requested it
    // 3. The query has more than 5 open queries (complex investigation)
    // Default: standard tier (fast, cheaper)
    return rc.IR != nil && rc.IR.ModelTier == "thinking"
}
```

---

## 10. Reviewer Prompt — Full Text

```
You are the Reviewer for a codebase intelligence engine. Your job is to
evaluate whether the current investigation has gathered enough evidence
to support a useful answer, and to learn from what the tools found.

You are NOT writing the final answer. You are deciding:
1. Has enough been gathered to synthesize a good answer?
2. What questions remain open?
3. What did the tools surface that should be recorded in the knowledge graph?

────────────────────────────────────────────────────────────────────────────
WHAT YOU RECEIVE
────────────────────────────────────────────────────────────────────────────

You receive:
- The original user query
- The compiled IR (anchors, open queries, predicates)
- The current loop index and max loops
- All tool emissions from this iteration (thinking channel content)
- All tool emissions from previous iterations (accumulated)

────────────────────────────────────────────────────────────────────────────
YOUR EVALUATION
────────────────────────────────────────────────────────────────────────────

Evaluate three things:

1. OPEN QUERIES RESOLVED
   For each open query in the IR, has the current evidence resolved it?
   "Resolved" means: the tool emissions contain enough specific information
   that the Synthesizer could answer this sub-question directly.
   Not resolved: the tools found the relevant code area but not the answer.

2. EVIDENCE DEPTH
   Is the evidence specific enough? Code-level specifics (function names,
   call chains, concrete types) are better than namespace-level generalities.
   If the evidence is vague, more investigation is needed.

3. DIMINISHING RETURNS
   Is this iteration adding meaningfully new information, or repeating
   what previous iterations already surfaced?
   If the same nodes keep appearing with no new relationships, converge.

────────────────────────────────────────────────────────────────────────────
ENRICHMENT DECISIONS
────────────────────────────────────────────────────────────────────────────

Tools may propose new nodes or edges. You decide whether to approve them.

Approve a proposed edge when:
- The relationship is clearly supported by the evidence in this iteration
- The source class is appropriate (speculative for inferred, structural for certain)
- Adding it would help future queries about this codebase

Reject a proposed edge when:
- The evidence is ambiguous about whether the relationship exists
- The edge would duplicate existing substrate structure
- The relationship seems incidental to this specific query

────────────────────────────────────────────────────────────────────────────
OUTPUT FORMAT
────────────────────────────────────────────────────────────────────────────

Think through your evaluation, then produce these XML tags:

<converged>true</converged>
<!-- true when: all open queries are resolved AND evidence is specific -->
<!-- true when: max loops reached (forced convergence) -->
<!-- false when: open queries remain OR evidence is too shallow -->

<open_queries>
  <!-- List only queries that remain UNRESOLVED after this iteration -->
  <!-- If all resolved: leave this empty -->
  <open_query>specific remaining question</open_query>
</open_queries>

<enrichments>
  <!-- Approved substrate changes. One per proposed node/edge. -->
  <!-- action: "approve" | "reject" -->
  <enrichment
    action="approve"
    entity_type="edge"
    entity_id="<edge-id-from-proposal>"
    rationale="clearly demonstrated by call chain evidence"/>
  <enrichment
    action="reject"
    entity_type="edge"
    entity_id="<edge-id>"
    rationale="evidence ambiguous — seen once, may be coincidental"/>
</enrichments>

────────────────────────────────────────────────────────────────────────────
CONVERGENCE RULES
────────────────────────────────────────────────────────────────────────────

Converge (set <converged>true</converged>) when ANY of these are true:

1. All open queries are resolved with specific evidence
2. The last two iterations surfaced no new nodes or relationships
3. Loop index equals max_loops (always converge — let Synthesizer handle partial)

Do NOT converge when:
- Open queries list things the tools haven't found yet
- The evidence is only at namespace level with no symbol-level specifics
- A tool explicitly failed and its findings are missing

────────────────────────────────────────────────────────────────────────────
RULES
────────────────────────────────────────────────────────────────────────────

1. Be decisive. Indecision wastes loops and context budget.
   If 70% of open queries are resolved and evidence is good, converge.

2. Do not approve enrichments for relationships you're uncertain about.
   Speculative edges are fine — spurious structural edges corrupt the graph.

3. If tools found nothing useful, note which open queries remain unresolved.
   The next iteration will start from the same anchors with the same predicates —
   consider whether the Strategizer's approach was wrong (note in open_queries).

4. If the query is fundamentally unanswerable from the substrate
   (e.g., asks about runtime behavior, not static structure), converge
   and the Synthesizer will explain the limitation.
```

### Reviewer Prompt Assembly

```go
// internal/agent/reviewer/reviewer.go

func (r *Node) buildPrompt(rc *runner.RunContext, loopEmissions []core.Emission) []llm.Message {
    // Assemble context: IR + accumulated emissions + this loop's emissions
    var context strings.Builder

    context.WriteString("## Original Query\n\n")
    context.WriteString(rc.Query)
    context.WriteString("\n\n")

    context.WriteString("## Investigation Plan (IR)\n\n")
    context.WriteString(fmt.Sprintf("Mode: %s\n", rc.IR.Mode))
    context.WriteString(fmt.Sprintf("Loop: %d/%d\n\n", rc.CurrentLoop(), rc.MaxLoops))

    context.WriteString("Open queries:\n")
    for _, q := range rc.IR.OpenQueries {
        context.WriteString(fmt.Sprintf("- %s\n", q))
    }
    context.WriteString("\n")

    // Previous iterations' emissions (summarized if long)
    prevEmissions := filterThinkingEmissions(rc.Emissions)
    if len(prevEmissions) > 0 {
        context.WriteString("## Previous Iterations\n\n")
        for _, e := range prevEmissions {
            context.WriteString(e.Content)
            context.WriteString("\n\n")
        }
    }

    // This iteration's emissions
    context.WriteString("## This Iteration (tool findings)\n\n")
    for _, e := range loopEmissions {
        if e.Channel == core.ChanThinking {
            context.WriteString(e.Content)
            context.WriteString("\n\n")
        }
    }

    // Proposed enrichments from tools
    proposed := extractProposedFromEmissions(loopEmissions)
    if len(proposed) > 0 {
        context.WriteString("## Proposed Substrate Changes\n\n")
        for _, p := range proposed {
            context.WriteString(fmt.Sprintf("- %s %s (id: %s)\n",
                p.EntityType, p.Action, p.EntityID))
        }
        context.WriteString("\n")
    }

    return []llm.Message{
        {Role: "user", Content: context.String()},
    }
}
```

---

## 11. Synthesizer Prompt — Full Text

### Full Convergence Prompt

```
You are the Synthesizer for a codebase intelligence engine. Your job is to
produce a clear, specific, grounded answer to the developer's query.

You have access to everything the investigation surfaced:
- The original query
- The compiled investigation plan
- All tool findings across all loop iterations
- The Reviewer's convergence assessment

────────────────────────────────────────────────────────────────────────────
HOW TO WRITE A GOOD ANSWER
────────────────────────────────────────────────────────────────────────────

A good answer from this engine is:

SPECIFIC — Names actual functions, types, files, and packages.
  Not: "the billing system handles this"
  Yes: "`ProcessPayment` in `internal/billing/invoice.go` calls
        `CreateBillingEvent` when the volunteer status is `CONFIRMED`"

GROUNDED — Every claim traces to specific evidence in the tool findings.
  Do not speculate. If something wasn't found, say so.

STRUCTURED — Use the natural structure of the findings.
  Code references in backticks.
  Call chains as lists.
  Multiple related answers in sections.

HONEST ABOUT GAPS — If the investigation didn't find something, say it.
  "The tool findings show the call chain up to `SchedulerService.Assign`
   but do not show what happens inside that method."

────────────────────────────────────────────────────────────────────────────
ANSWER STRUCTURE GUIDELINES
────────────────────────────────────────────────────────────────────────────

For simple questions (what does X do, where is Y defined):
  Answer in 2-4 sentences with code references. No sections needed.

For architectural questions (how does A connect to B):
  1. Direct answer (1-2 sentences)
  2. Evidence — the specific call chain, edge, or relationship found
  3. Related context — what else the investigation surfaced

For "how does X work" questions:
  1. Brief description of the mechanism
  2. Entry point(s)
  3. Key steps with code references
  4. Exit points / return values
  5. Notable dependencies

────────────────────────────────────────────────────────────────────────────
RULES
────────────────────────────────────────────────────────────────────────────

1. Never invent code details. Only reference what appeared in the tool findings.

2. If the tool findings are incomplete, say so explicitly:
   "The investigation found X but did not surface Y — a follow-up query
    focused on Y would give more detail."

3. Reference canonical IDs (package/path:Symbol) for functions and types.
   This lets the developer click through to the code.

4. If multiple tools found conflicting information, note the discrepancy.

5. Do not explain how the engine works or reference the investigation process.
   Answer as if you just know the codebase.
```

### Partial Answer Prompt (forced exit)

```
You are the Synthesizer for a codebase intelligence engine. The investigation
was cut short before it could complete — either the context window was filling
up, or the loop limit was reached.

You must produce a PARTIAL answer that is honest about its limitations.

────────────────────────────────────────────────────────────────────────────
STRUCTURE FOR PARTIAL ANSWERS
────────────────────────────────────────────────────────────────────────────

A partial answer has three sections:

1. WHAT WAS FOUND
   Answer the original query as fully as you can from the evidence gathered.
   Use the same quality standards as a full answer — specific, grounded.
   Do not pad this with speculation.

2. WHAT REMAINS UNKNOWN
   List the open queries that were NOT resolved by the investigation.
   Be specific: "Did not determine how billing event type is selected" is
   better than "investigation incomplete".

3. HOW TO GET THE REST
   Suggest a more focused follow-up query that would surface the missing
   information. This should be a concrete `ce query "..."` suggestion.

────────────────────────────────────────────────────────────────────────────
RULES
────────────────────────────────────────────────────────────────────────────

1. Never pretend the answer is complete when it isn't.
   The partial answer notice is added automatically — do not add your own.

2. What you found should still be useful. If the investigation surfaced
   useful evidence before being cut short, present it clearly.

3. The follow-up query suggestion should be more focused than the original —
   it should target specifically what was not found.
```

### Synthesizer Prompt Assembly

```go
// internal/agent/synthesizer/synthesizer.go

func (s *Node) buildPrompt(rc *runner.RunContext, partial bool) []llm.Message {
    var context strings.Builder

    context.WriteString("## Original Query\n\n")
    context.WriteString(rc.Query)
    context.WriteString("\n\n")

    context.WriteString("## Investigation Plan\n\n")
    context.WriteString(fmt.Sprintf("Mode: %s | Loops completed: %d\n\n",
        rc.IR.Mode, rc.CurrentLoop()))

    if partial {
        context.WriteString("## Unresolved Questions\n\n")
        for _, q := range rc.IR.OpenQueries {
            context.WriteString(fmt.Sprintf("- %s\n", q))
        }
        context.WriteString("\n")
        context.WriteString(fmt.Sprintf("Exit reason: %s\n\n", rc.ForcedExitReason))
    }

    context.WriteString("## Tool Findings\n\n")
    for _, e := range rc.Emissions {
        if e.Channel == core.ChanThinking {
            context.WriteString(e.Content)
            context.WriteString("\n\n")
        }
    }

    return []llm.Message{
        {Role: "user", Content: context.String()},
    }
}

func (s *Node) systemPrompt(rc *runner.RunContext) string {
    if rc.ForcedExit {
        return synthesizerPartialPrompt
    }
    return synthesizerFullPrompt
}
```

---

## 12. Node Tier Constants

```go
// internal/core/constants.go (additions)

const (
    TierFast     = "fast"
    TierStandard = "standard"
    TierThinking = "thinking"
)
```

---

## 13. Package Layout Summary

```
internal/llm/
  router.go           — Router, NewRouter(), Complete(), ModelForTier()
  types.go            — CompletionRequest, CompletionResponse, ModelInfo,
                        Message, ThinkingConfig
  budget.go           — EstimateCost()
  anthropic/
    provider.go       — AnthropicProvider, Complete(), doRequest(), parseResponse()
    retry.go          — Retrier, APIError, IsRetryable()
    thinking.go       — extended thinking config helpers
    models.go         — Models map, DefaultModels map
  openai/
    provider.go       — stub (Phase 2)
  local/
    provider.go       — stub, Ollama base URL

internal/agent/reviewer/
  reviewer.go         — Node, Run(), buildPrompt(), parseResponse()
  prompt.go           — reviewerSystemPrompt (the full prompt text above)

internal/agent/synthesizer/
  synthesizer.go      — Node, Run(), runFull(), runPartial(), buildPrompt()
  prompt.go           — synthesizerFullPrompt, synthesizerPartialPrompt
```

---

## 14. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| Primary provider | Anthropic (Claude) |
| Fast tier | claude-haiku-4-5-20251001 |
| Standard tier | claude-sonnet-4-6 |
| Thinking tier | claude-opus-4-6 |
| Extended thinking | Strategizer only, thinking tier only, 8K budget tokens |
| Thinking trace | Logged to execution.db + emitted to ChanThinking |
| Retry policy | 3 retries, exponential backoff (1s/2s/4s), ±20% jitter |
| Retryable errors | 429, 500, 502, 503, 529 |
| Cost tracking | EstimateCost() per response, accumulated in Budget |
| Reviewer tier | Fast (haiku) — convergence check, not deep reasoning |
| Synthesizer tier | Standard (sonnet) — quality answer generation |
| Strategizer tier | Standard default, thinking on complex queries |
| Partial answer | Three-section structure: found / unknown / follow-up query |
| Convergence rule | Converge at 70%+ open queries resolved with specific evidence |
| OpenAI provider | Stub for Phase 2, full implementation Phase 3 |
| Local provider | Stub for Phase 2 (Ollama), full implementation Phase 3 |

---

*Spec 12: LLM Provider, Router, Reviewer & Synthesizer Prompts — v1.0 — February 2026*
*All twelve specs complete. Engine is fully specced.*
*Companion: Context Engine PRD v0.5 Sections 9.3, 9.4, 11 | Decisions Log v1.0 Section 7*
