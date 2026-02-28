# Context Engine — Spec 15: CE Studio
## Implementation Spec — React Web Application, Cognitive Trace, Graph Explorer
### Version 1.0 | February 2026

---

> This spec covers CE Studio — the web UI for the Context Engine.
> Studio is a separate repository from the engine.
> Hand to Claude Code alongside spec-14-server.md (API contract).
> Companion: Context Engine PRD v0.5 Section 16. Decisions Log v1.0 Section 11.
> Read spec-public/frontend-design/SKILL.md before building any UI component.

---

## 1. Overview

CE Studio is the full product surface for users who don't live in the
terminal. It serves three roles:

1. **Query Interface** — run investigations, watch the cognitive loop in
   real time, read answers. Everything the TUI does, in a browser.

2. **Trace Inspector** — replay any past query's cognitive loop. See exactly
   what the Strategizer planned, what each tool found, what the Reviewer
   approved, how the Synthesizer constructed the answer. The engine's
   reasoning made visible.

3. **Graph Explorer** — navigate the substrate graph. Browse nodes by type,
   search by name, inspect edges and weights, see activation history. The
   knowledge graph made tangible.

Studio connects to a running `ce server` via the REST API and WebSocket
from Spec 14.

---

## 2. Technology Stack

| Concern | Choice | Rationale |
|---------|--------|-----------|
| Framework | Next.js 14 (App Router) | SSR for initial load, client components for live UI |
| Language | TypeScript | Consistent with plugin SDK |
| Styling | Tailwind CSS | Utility-first, consistent with design system |
| UI components | shadcn/ui | Accessible, unstyled base, customizable |
| Graph rendering | Cytoscape.js | Best-in-class for property graphs, handles 10k+ nodes |
| Code highlighting | Shiki | Same highlighter as VS Code, accurate for all languages |
| State management | Zustand | Simple, minimal, no boilerplate |
| WebSocket client | Native WebSocket + custom hook | No library needed |
| Markdown | react-markdown + remark-gfm | Answer rendering |
| Icons | Lucide React | Consistent with shadcn |
| Package manager | pnpm | Consistent with plugin SDK |

---

## 3. Repository Structure

```
ce-studio/
  app/
    layout.tsx            — root layout, providers
    page.tsx              — home → redirect to /query
    query/
      page.tsx            — query interface
      [runId]/
        page.tsx          — trace inspector for a specific run
    graph/
      page.tsx            — graph explorer
    history/
      page.tsx            — query history
    settings/
      page.tsx            — CE server connection, token management
  components/
    query/
      QueryInput.tsx      — query input with submit
      StreamView.tsx      — live streaming thought process
      AnswerView.tsx      — rendered answer with code highlighting
      LoopProgress.tsx    — loop progress bar + cost + elapsed
      ToolIndicators.tsx  — tool status indicators
    trace/
      TraceTimeline.tsx   — loop-by-loop timeline
      LLMCallCard.tsx     — expandable LLM call with prompt + response
      StrategyCard.tsx    — Strategizer IR visualization
      ReviewCard.tsx      — Reviewer convergence decision
      SynthCard.tsx       — Synthesizer output
      ThinkingBlock.tsx   — extended thinking display
    graph/
      GraphCanvas.tsx     — Cytoscape.js graph renderer
      NodePanel.tsx       — selected node details
      EdgePanel.tsx       — selected edge details
      GraphControls.tsx   — filter, zoom, layout controls
      ActivationHeatmap.tsx — activation overlay on graph
    shared/
      CodeBlock.tsx       — Shiki syntax highlighted code
      MarkdownView.tsx    — react-markdown answer renderer
      CostBadge.tsx       — token cost display
      StatusDot.tsx       — colored status indicator
      EmptyState.tsx      — empty state with action
  lib/
    api.ts                — typed API client (REST)
    ws.ts                 — WebSocket client hook
    store.ts              — Zustand store
    types.ts              — TypeScript types (mirrors engine API types)
    format.ts             — formatCost(), formatDuration(), etc.
  hooks/
    useQuery.ts           — query execution + streaming
    useExecLog.ts         — execution log fetching
    useSubstrate.ts       — substrate data fetching
    useServerStatus.ts    — server health polling
```

---

## 4. Application Store

```typescript
// lib/store.ts

import { create } from "zustand"
import { persist } from "zustand/middleware"

interface ServerConfig {
  baseURL:  string  // e.g., "http://localhost:8765"
  token:    string  // Bearer token (empty for local no-auth)
}

interface ActiveQuery {
  id:         string
  query:      string
  status:     "running" | "complete" | "error"
  events:     StreamEvent[]
  answer:     string
  cost:       number
  loops:      number
  maxLoops:   number
  startTime:  number
  endTime?:   number
  partial:    boolean
}

interface AppState {
  // Server connection
  server:      ServerConfig
  connected:   boolean
  setServer:   (cfg: ServerConfig) => void
  setConnected: (connected: boolean) => void

  // Active query (running or most recent)
  activeQuery: ActiveQuery | null
  setActiveQuery: (q: ActiveQuery | null) => void
  appendEvent:    (event: StreamEvent) => void

  // Navigation
  selectedRunId: string | null
  setSelectedRunId: (id: string | null) => void

  // Graph explorer state
  graphProjectId:  string | null
  graphNodeFilter: { type?: string; search?: string; minActivation?: number }
  setGraphProjectId: (id: string | null) => void
  setGraphNodeFilter: (f: Partial<AppState["graphNodeFilter"]>) => void
}

export const useStore = create<AppState>()(
  persist(
    (set, get) => ({
      server: {
        baseURL: "http://localhost:8765",
        token:   "",
      },
      connected: false,
      setServer:    (cfg) => set({ server: cfg }),
      setConnected: (connected) => set({ connected }),

      activeQuery: null,
      setActiveQuery: (q) => set({ activeQuery: q }),
      appendEvent: (event) => set((state) => {
        if (!state.activeQuery) return state
        return {
          activeQuery: {
            ...state.activeQuery,
            events: [...state.activeQuery.events, event],
            ...(event.type === "message" ? { answer: event.content } : {}),
            ...(event.type === "cost" ? { cost: parseCost(event.content) } : {}),
            ...(event.type === "done" ? {
              status: "complete",
              endTime: Date.now(),
            } : {}),
          }
        }
      }),

      selectedRunId: null,
      setSelectedRunId: (id) => set({ selectedRunId: id }),

      graphProjectId: null,
      graphNodeFilter: {},
      setGraphProjectId: (id) => set({ graphProjectId: id }),
      setGraphNodeFilter: (f) => set((state) => ({
        graphNodeFilter: { ...state.graphNodeFilter, ...f }
      })),
    }),
    {
      name: "ce-studio",
      partialize: (state) => ({
        server: state.server,
        // Don't persist activeQuery or graph state
      }),
    }
  )
)
```

---

## 5. API Client

```typescript
// lib/api.ts

export class CEApiClient {
  private baseURL: string
  private token:   string

  constructor(baseURL: string, token: string) {
    this.baseURL = baseURL.replace(/\/$/, "")
    this.token   = token
  }

  private headers(): HeadersInit {
    const h: HeadersInit = { "Content-Type": "application/json" }
    if (this.token) {
      h["Authorization"] = `Bearer ${this.token}`
    }
    return h
  }

  private async fetch<T>(path: string, init?: RequestInit): Promise<T> {
    const resp = await fetch(this.baseURL + path, {
      ...init,
      headers: { ...this.headers(), ...init?.headers },
    })
    if (!resp.ok) {
      const body = await resp.text()
      throw new APIError(resp.status, body)
    }
    return resp.json()
  }

  // ── Health ────────────────────────────────────────────────────────────
  async health(): Promise<{ status: string }> {
    return this.fetch("/health")
  }

  // ── Projects ──────────────────────────────────────────────────────────
  async listProjects(): Promise<Project[]> {
    return this.fetch("/api/v1/projects")
  }

  async getProject(id: string): Promise<Project> {
    return this.fetch(`/api/v1/projects/${id}`)
  }

  // ── Query (sync) ──────────────────────────────────────────────────────
  async query(req: QueryRequest): Promise<QueryResponse> {
    return this.fetch("/api/v1/query", {
      method: "POST",
      body: JSON.stringify(req),
    })
  }

  // ── Execution log ─────────────────────────────────────────────────────
  async listExecLog(params: ExecLogParams): Promise<ExecLogList> {
    const qs = new URLSearchParams({
      limit:  String(params.limit ?? 20),
      offset: String(params.offset ?? 0),
      ...(params.projectId ? { project_id: params.projectId } : {}),
    })
    return this.fetch(`/api/v1/execlog?${qs}`)
  }

  async getExecRun(runId: string): Promise<ExecRunDetail> {
    return this.fetch(`/api/v1/execlog/${runId}`)
  }

  // ── Substrate ─────────────────────────────────────────────────────────
  async getNodes(params: NodeQueryParams): Promise<NodesResponse> {
    const qs = new URLSearchParams({
      project_id: params.projectId,
      limit:  String(params.limit ?? 50),
      offset: String(params.offset ?? 0),
      ...(params.type   ? { type:   params.type }   : {}),
      ...(params.search ? { search: params.search } : {}),
      ...(params.minActivation != null
        ? { min_activation: String(params.minActivation) } : {}),
    })
    return this.fetch(`/api/v1/substrate/nodes?${qs}`)
  }

  async getEdges(params: EdgeQueryParams): Promise<EdgesResponse> {
    const qs = new URLSearchParams({
      project_id: params.projectId,
      ...(params.nodeId ? { node_id: params.nodeId } : {}),
      limit: String(params.limit ?? 100),
    })
    return this.fetch(`/api/v1/substrate/edges?${qs}`)
  }
}

export class APIError extends Error {
  constructor(public status: number, message: string) {
    super(message)
  }
}
```

---

## 6. WebSocket Hook

```typescript
// hooks/useQuery.ts

import { useRef, useCallback } from "react"
import { useStore } from "@/lib/store"
import type { StreamEvent } from "@/lib/types"

export function useQuery() {
  const { server, setActiveQuery, appendEvent } = useStore()
  const wsRef = useRef<WebSocket | null>(null)

  const runQuery = useCallback((query: string, maxLoops?: number) => {
    // Cancel any in-flight query
    if (wsRef.current) {
      wsRef.current.close()
    }

    const id = crypto.randomUUID()

    setActiveQuery({
      id,
      query,
      status:    "running",
      events:    [],
      answer:    "",
      cost:      0,
      loops:     0,
      maxLoops:  maxLoops ?? 8,
      startTime: Date.now(),
      partial:   false,
    })

    const wsURL = server.baseURL
      .replace(/^http/, "ws")
      .replace(/\/$/, "") + "/api/v1/ws"

    const tokenParam = server.token
      ? `?token=${encodeURIComponent(server.token)}`
      : ""

    const ws = new WebSocket(wsURL + tokenParam)
    wsRef.current = ws

    ws.onopen = () => {
      ws.send(JSON.stringify({ query, max_loops: maxLoops }))
    }

    ws.onmessage = (evt) => {
      const event: StreamEvent = JSON.parse(evt.data)
      appendEvent(event)

      // Parse loop progress from system events
      if (event.type === "system" && event.metadata?.loop_index) {
        useStore.getState().setActiveQuery({
          ...useStore.getState().activeQuery!,
          loops: event.metadata.loop_index as number,
        })
      }
    }

    ws.onerror = () => {
      appendEvent({ type: "error", content: "WebSocket connection failed" })
      useStore.getState().setActiveQuery({
        ...useStore.getState().activeQuery!,
        status: "error",
        endTime: Date.now(),
      })
    }

    ws.onclose = () => {
      wsRef.current = null
    }
  }, [server, setActiveQuery, appendEvent])

  const cancelQuery = useCallback(() => {
    wsRef.current?.close()
    wsRef.current = null
    const q = useStore.getState().activeQuery
    if (q?.status === "running") {
      useStore.getState().setActiveQuery({
        ...q,
        status: "error",
        endTime: Date.now(),
      })
    }
  }, [])

  return { runQuery, cancelQuery }
}
```

---

## 7. Query Page

```typescript
// app/query/page.tsx
"use client"

export default function QueryPage() {
  const { activeQuery } = useStore()
  const { runQuery, cancelQuery } = useQuery()

  return (
    <div className="flex flex-col h-screen bg-gray-950">
      {/* Top bar */}
      <QueryTopBar
        query={activeQuery?.query ?? ""}
        loops={activeQuery?.loops ?? 0}
        maxLoops={activeQuery?.maxLoops ?? 0}
        cost={activeQuery?.cost ?? 0}
        elapsed={activeQuery ? Date.now() - activeQuery.startTime : 0}
        status={activeQuery?.status}
        onCancel={cancelQuery}
      />

      {/* Main content */}
      <div className="flex-1 overflow-hidden">
        {!activeQuery ? (
          <QueryWelcome onSubmit={runQuery} />
        ) : activeQuery.status === "running" ? (
          <StreamView
            events={activeQuery.events}
            query={activeQuery.query}
          />
        ) : (
          <AnswerView
            answer={activeQuery.answer}
            events={activeQuery.events}
            runId={activeQuery.id}
            partial={activeQuery.partial}
          />
        )}
      </div>

      {/* Bottom input (always visible) */}
      <QueryInput onSubmit={runQuery} disabled={activeQuery?.status === "running"} />
    </div>
  )
}
```

---

## 8. Stream View Component

Renders the cognitive loop as it runs. Groups events by loop iteration.

```typescript
// components/query/StreamView.tsx
"use client"

interface LoopGroup {
  index:    number
  events:   StreamEvent[]
  complete: boolean
}

export function StreamView({ events, query }: { events: StreamEvent[]; query: string }) {
  const bottomRef = useRef<HTMLDivElement>(null)

  // Group events by loop iteration
  const loopGroups = useMemo(() => groupEventsByLoop(events), [events])

  // Auto-scroll to bottom
  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [events.length])

  return (
    <div className="h-full overflow-y-auto px-6 py-4 space-y-6">

      {/* Strategizer output (loop 0 / pre-loop) */}
      {loopGroups.strategizer && (
        <StrategyCard events={loopGroups.strategizer} />
      )}

      {/* Loop iterations */}
      {loopGroups.loops.map((loop) => (
        <LoopCard key={loop.index} loop={loop} />
      ))}

      {/* Auto-scroll anchor */}
      <div ref={bottomRef} />
    </div>
  )
}

function LoopCard({ loop }: { loop: LoopGroup }) {
  const [expanded, setExpanded] = useState(true)

  const thinkingEvents = loop.events.filter(e => e.type === "thinking")
  const actionEvents   = loop.events.filter(e => e.type === "action")

  return (
    <div className="border border-gray-800 rounded-lg overflow-hidden">
      {/* Loop header */}
      <button
        className="w-full flex items-center gap-3 px-4 py-2 bg-gray-900
                   hover:bg-gray-800 transition-colors text-left"
        onClick={() => setExpanded(!expanded)}
      >
        <span className="text-violet-400 font-mono text-sm">
          Loop {loop.index}
        </span>
        <ToolBadges events={actionEvents} />
        {loop.complete && <span className="ml-auto text-emerald-400 text-xs">✓</span>}
        {!loop.complete && <Spinner className="ml-auto w-3 h-3 text-violet-400" />}
      </button>

      {/* Loop content */}
      {expanded && (
        <div className="px-4 py-3 space-y-2 border-t border-gray-800">
          {loop.events.map((event, i) => (
            <EventLine key={i} event={event} />
          ))}
        </div>
      )}
    </div>
  )
}

function EventLine({ event }: { event: StreamEvent }) {
  switch (event.type) {
    case "thinking":
      return (
        <p className="text-gray-400 text-sm font-mono pl-4 border-l border-gray-700">
          {event.content}
        </p>
      )
    case "action":
      return (
        <p className="text-violet-300 text-sm">
          <span className="text-gray-500 mr-2">•</span>
          {event.content}
        </p>
      )
    case "warning":
      return (
        <p className="text-amber-400 text-sm">
          <span className="mr-2">⚠</span>
          {event.content}
        </p>
      )
    case "error":
      return (
        <p className="text-red-400 text-sm">
          <span className="mr-2">✗</span>
          {event.content}
        </p>
      )
    default:
      return null
  }
}
```

---

## 9. Answer View Component

```typescript
// components/query/AnswerView.tsx
"use client"

export function AnswerView({
  answer, events, runId, partial
}: {
  answer: string
  events: StreamEvent[]
  runId:  string
  partial: boolean
}) {
  const [showTrace, setShowTrace] = useState(false)
  const router = useRouter()

  return (
    <div className="h-full flex flex-col">
      {/* Answer header */}
      <div className="flex items-center gap-4 px-6 py-3 border-b border-gray-800">
        <h2 className="text-gray-200 font-semibold">Answer</h2>
        {partial && (
          <span className="text-amber-400 text-xs px-2 py-0.5 rounded
                           border border-amber-800 bg-amber-950">
            partial
          </span>
        )}
        <div className="ml-auto flex items-center gap-3">
          <button
            className="text-gray-400 hover:text-gray-200 text-sm transition-colors"
            onClick={() => setShowTrace(!showTrace)}
          >
            {showTrace ? "hide trace" : "show trace"}
          </button>
          <button
            className="text-gray-400 hover:text-gray-200 text-sm transition-colors"
            onClick={() => router.push(`/query/${runId}`)}
          >
            full inspector →
          </button>
        </div>
      </div>

      <div className="flex-1 overflow-hidden flex">
        {/* Answer content */}
        <div className={`overflow-y-auto px-6 py-4
                        ${showTrace ? "w-1/2" : "w-full"}`}>
          <MarkdownView content={answer} />
        </div>

        {/* Inline trace panel */}
        {showTrace && (
          <div className="w-1/2 border-l border-gray-800 overflow-y-auto">
            <StreamView events={events} query="" />
          </div>
        )}
      </div>
    </div>
  )
}
```

---

## 10. Trace Inspector Page

Full-detail view of a past query run. Navigated to from history or the
"full inspector" link on the answer view.

```typescript
// app/query/[runId]/page.tsx
"use client"

export default function TraceInspectorPage({
  params
}: {
  params: { runId: string }
}) {
  const { data: run, isLoading } = useExecLog(params.runId)

  if (isLoading) return <LoadingState />
  if (!run) return <NotFound />

  return (
    <div className="flex h-screen bg-gray-950">

      {/* Left panel: timeline */}
      <div className="w-64 border-r border-gray-800 overflow-y-auto">
        <TraceTimeline run={run} />
      </div>

      {/* Main panel: selected call detail */}
      <div className="flex-1 overflow-y-auto p-6">
        <TraceDetail run={run} />
      </div>

    </div>
  )
}
```

### Trace Timeline

```typescript
// components/trace/TraceTimeline.tsx

export function TraceTimeline({ run }: { run: ExecRunDetail }) {
  const [selected, setSelected] = useState<string | null>(null)

  // Group LLM calls by loop
  const byLoop = groupCallsByLoop(run.llmCalls)

  return (
    <div className="p-4 space-y-4">
      <div className="text-gray-400 text-xs font-mono truncate">
        {run.query}
      </div>
      <div className="text-gray-600 text-xs">
        {run.loopsUsed} loops · {formatCost(run.costUSD)} · {formatDuration(run.durationMS)}
      </div>

      <div className="space-y-2">
        {byLoop.map(({ loopIndex, calls }) => (
          <div key={loopIndex} className="space-y-1">
            <div className="text-gray-600 text-xs">
              {loopIndex === 0 ? "strategizer" : `loop ${loopIndex}`}
            </div>
            {calls.map(call => (
              <button
                key={call.callID}
                className={`w-full text-left px-2 py-1.5 rounded text-xs
                            transition-colors
                            ${selected === call.callID
                              ? "bg-violet-900/50 text-violet-200"
                              : "text-gray-400 hover:bg-gray-800"}`}
                onClick={() => setSelected(call.callID)}
              >
                <span className="font-mono">{call.nodeType}</span>
                <span className="ml-2 text-gray-600">
                  {call.tokensIn + call.tokensOut}t
                </span>
              </button>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}
```

### LLM Call Card

```typescript
// components/trace/LLMCallCard.tsx

export function LLMCallCard({ call }: { call: LLMCallLog }) {
  const [tab, setTab] = useState<"response" | "prompt" | "thinking">("response")

  return (
    <div className="border border-gray-800 rounded-lg overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-4 px-4 py-3 bg-gray-900">
        <span className="text-violet-400 font-mono text-sm">{call.nodeType}</span>
        <span className="text-gray-500 text-xs">{call.model}</span>
        <span className="text-gray-500 text-xs">
          {call.tokensIn}↑ {call.tokensOut}↓ tokens
        </span>
        <span className="text-gray-500 text-xs">{call.latencyMs}ms</span>

        {call.thinkingText && (
          <span className="ml-auto text-xs text-violet-400 px-2 py-0.5
                           border border-violet-800 rounded">
            extended thinking
          </span>
        )}
      </div>

      {/* Tabs */}
      <div className="flex border-b border-gray-800">
        {(["response", "prompt", ...(call.thinkingText ? ["thinking"] : [])] as const).map(t => (
          <button
            key={t}
            className={`px-4 py-2 text-xs transition-colors
                        ${tab === t
                          ? "text-gray-200 border-b-2 border-violet-500"
                          : "text-gray-500 hover:text-gray-300"}`}
            onClick={() => setTab(t as any)}
          >
            {t}
          </button>
        ))}
      </div>

      {/* Content */}
      <div className="p-4 max-h-96 overflow-y-auto">
        {tab === "response" && (
          call.nodeType === "synthesizer"
            ? <MarkdownView content={call.response} />
            : <pre className="text-gray-300 text-xs font-mono whitespace-pre-wrap">
                {call.response}
              </pre>
        )}
        {tab === "prompt" && (
          <div className="space-y-4">
            <div>
              <div className="text-gray-500 text-xs mb-2">system</div>
              <pre className="text-gray-400 text-xs font-mono whitespace-pre-wrap">
                {call.systemPrompt}
              </pre>
            </div>
            <div>
              <div className="text-gray-500 text-xs mb-2">user</div>
              <pre className="text-gray-300 text-xs font-mono whitespace-pre-wrap">
                {call.userMessage}
              </pre>
            </div>
          </div>
        )}
        {tab === "thinking" && call.thinkingText && (
          <div className="text-violet-300 text-xs font-mono whitespace-pre-wrap
                          bg-violet-950/30 rounded p-3">
            {call.thinkingText}
          </div>
        )}
      </div>
    </div>
  )
}
```

---

## 11. Graph Explorer Page

```typescript
// app/graph/page.tsx
"use client"

export default function GraphPage() {
  const { graphProjectId, graphNodeFilter, setGraphNodeFilter } = useStore()
  const { nodes, edges, isLoading } = useSubstrate(graphProjectId, graphNodeFilter)
  const [selectedNode, setSelectedNode] = useState<NodeResponse | null>(null)

  return (
    <div className="flex h-screen bg-gray-950">

      {/* Left sidebar: controls + selected node */}
      <div className="w-72 border-r border-gray-800 flex flex-col">
        <GraphControls
          filter={graphNodeFilter}
          onFilterChange={setGraphNodeFilter}
        />
        {selectedNode && (
          <NodePanel
            node={selectedNode}
            onClose={() => setSelectedNode(null)}
          />
        )}
      </div>

      {/* Graph canvas */}
      <div className="flex-1 relative">
        {isLoading ? (
          <GraphLoadingState />
        ) : (
          <GraphCanvas
            nodes={nodes}
            edges={edges}
            onNodeSelect={setSelectedNode}
          />
        )}
      </div>

    </div>
  )
}
```

### Graph Canvas (Cytoscape.js)

```typescript
// components/graph/GraphCanvas.tsx
"use client"

import CytoscapeComponent from "react-cytoscapejs"
import cytoscape from "cytoscape"
import fcose from "cytoscape-fcose"  // force-directed layout, handles dense graphs

cytoscape.use(fcose)

export function GraphCanvas({
  nodes, edges, onNodeSelect
}: {
  nodes: NodeResponse[]
  edges: EdgeResponse[]
  onNodeSelect: (node: NodeResponse) => void
}) {
  const cyRef = useRef<cytoscape.Core | null>(null)

  const elements = useMemo(() => [
    ...nodes.map(n => ({
      data: {
        id:           n.id,
        label:        n.label,
        type:         n.type,
        canonicalID:  n.canonicalId,
        activation:   n.activation,
        sourceClass:  n.sourceClass,
      }
    })),
    ...edges.map(e => ({
      data: {
        id:     e.id,
        source: e.sourceId,
        target: e.targetId,
        type:   e.type,
        weight: e.weight,
      }
    })),
  ], [nodes, edges])

  const stylesheet: cytoscape.Stylesheet[] = [
    {
      selector: "node",
      style: {
        "label":            "data(label)",
        "font-size":        "10px",
        "color":            "#9CA3AF",
        "text-valign":      "bottom",
        "text-margin-y":    4,
        "width":            nodeSize,
        "height":           nodeSize,
        "background-color": nodeColor,  // function based on type
        "border-width":     activationBorder,  // function based on activation
        "border-color":     "#7C3AED",
      }
    },
    {
      selector: "node[type = 'symbol']",
      style: { "background-color": "#1D4ED8" }
    },
    {
      selector: "node[type = 'namespace']",
      style: { "background-color": "#065F46" }
    },
    {
      selector: "node[type = 'concept']",
      style: { "background-color": "#7C3AED" }
    },
    {
      selector: "node[type = 'file']",
      style: { "background-color": "#374151" }
    },
    {
      selector: "node:selected",
      style: {
        "border-width": 3,
        "border-color": "#A78BFA",
      }
    },
    {
      selector: "edge",
      style: {
        "width":            "mapData(weight, 0, 1, 0.5, 3)",
        "line-color":       "#374151",
        "target-arrow-color": "#374151",
        "target-arrow-shape": "triangle",
        "curve-style":      "bezier",
        "opacity":          0.6,
      }
    },
    {
      selector: "edge[sourceClass = 'structural']",
      style: { "line-color": "#1D4ED8", "target-arrow-color": "#1D4ED8" }
    },
    {
      selector: "edge[sourceClass = 'associative']",
      style: { "line-color": "#065F46", "target-arrow-color": "#065F46" }
    },
  ]

  return (
    <CytoscapeComponent
      elements={elements}
      stylesheet={stylesheet}
      layout={{ name: "fcose", animate: true, randomize: false }}
      cy={(cy) => {
        cyRef.current = cy
        cy.on("tap", "node", (evt) => {
          const nodeId = evt.target.id()
          const node = nodes.find(n => n.id === nodeId)
          if (node) onNodeSelect(node)
        })
      }}
      style={{ width: "100%", height: "100%" }}
    />
  )
}
```

### Graph Controls

```typescript
// components/graph/GraphControls.tsx

export function GraphControls({ filter, onFilterChange }: {
  filter: GraphFilter
  onFilterChange: (f: Partial<GraphFilter>) => void
}) {
  return (
    <div className="p-4 space-y-4 border-b border-gray-800">
      <h3 className="text-gray-300 text-sm font-semibold">Graph Explorer</h3>

      {/* Search */}
      <input
        type="text"
        placeholder="Search nodes..."
        className="w-full bg-gray-900 border border-gray-700 rounded px-3 py-1.5
                   text-sm text-gray-200 placeholder-gray-600
                   focus:outline-none focus:border-violet-500"
        value={filter.search ?? ""}
        onChange={e => onFilterChange({ search: e.target.value })}
      />

      {/* Node type filter */}
      <div className="space-y-1">
        <div className="text-gray-500 text-xs">Node type</div>
        <div className="flex flex-wrap gap-1">
          {["symbol", "namespace", "concept", "file"].map(type => (
            <button
              key={type}
              className={`px-2 py-0.5 rounded text-xs transition-colors
                          ${filter.type === type
                            ? "bg-violet-700 text-violet-100"
                            : "bg-gray-800 text-gray-400 hover:bg-gray-700"}`}
              onClick={() => onFilterChange({
                type: filter.type === type ? undefined : type
              })}
            >
              {type}
            </button>
          ))}
        </div>
      </div>

      {/* Activation filter */}
      <div className="space-y-1">
        <div className="text-gray-500 text-xs">
          Min activation: {filter.minActivation?.toFixed(2) ?? "0.00"}
        </div>
        <input
          type="range"
          min="0" max="1" step="0.05"
          value={filter.minActivation ?? 0}
          onChange={e => onFilterChange({ minActivation: parseFloat(e.target.value) })}
          className="w-full accent-violet-500"
        />
      </div>
    </div>
  )
}
```

---

## 12. Query History Page

```typescript
// app/history/page.tsx
"use client"

export default function HistoryPage() {
  const { server } = useStore()
  const client = new CEApiClient(server.baseURL, server.token)
  const [runs, setRuns] = useState<ExecRunSummary[]>([])

  useEffect(() => {
    client.listExecLog({ limit: 50 }).then(r => setRuns(r.runs))
  }, [])

  return (
    <div className="max-w-3xl mx-auto py-8 px-6">
      <h1 className="text-gray-200 text-xl font-semibold mb-6">Query History</h1>
      <div className="space-y-2">
        {runs.map(run => (
          <RunRow key={run.runId} run={run} />
        ))}
      </div>
    </div>
  )
}

function RunRow({ run }: { run: ExecRunSummary }) {
  const router = useRouter()

  return (
    <button
      className="w-full text-left p-4 rounded-lg border border-gray-800
                 hover:border-gray-600 transition-colors bg-gray-900/50"
      onClick={() => router.push(`/query/${run.runId}`)}
    >
      <div className="flex items-start justify-between gap-4">
        <p className="text-gray-200 text-sm line-clamp-1">{run.query}</p>
        <div className="flex items-center gap-3 shrink-0">
          {run.partial && (
            <span className="text-amber-400 text-xs">partial</span>
          )}
          <span className="text-gray-500 text-xs">{formatCost(run.costUsd)}</span>
          <span className="text-gray-500 text-xs">{formatRelativeTime(run.startedAt)}</span>
        </div>
      </div>
      <div className="mt-1 text-gray-600 text-xs">
        {run.loopsUsed} loops · {run.tokensIn + run.tokensOut} tokens
      </div>
    </button>
  )
}
```

---

## 13. Settings Page

```typescript
// app/settings/page.tsx
"use client"

export default function SettingsPage() {
  const { server, setServer } = useStore()
  const { connected } = useServerStatus()
  const [form, setForm] = useState(server)

  return (
    <div className="max-w-xl mx-auto py-8 px-6">
      <h1 className="text-gray-200 text-xl font-semibold mb-6">Settings</h1>

      <div className="space-y-6">
        {/* Server connection */}
        <section className="space-y-4">
          <h2 className="text-gray-300 text-sm font-medium">CE Server</h2>

          <div className="space-y-1">
            <label className="text-gray-500 text-xs">Server URL</label>
            <input
              type="text"
              className="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2
                         text-sm text-gray-200 focus:outline-none focus:border-violet-500"
              value={form.baseURL}
              onChange={e => setForm(f => ({ ...f, baseURL: e.target.value }))}
            />
          </div>

          <div className="space-y-1">
            <label className="text-gray-500 text-xs">API Token (optional for local)</label>
            <input
              type="password"
              className="w-full bg-gray-900 border border-gray-700 rounded px-3 py-2
                         text-sm text-gray-200 focus:outline-none focus:border-violet-500"
              value={form.token}
              placeholder="ce_..."
              onChange={e => setForm(f => ({ ...f, token: e.target.value }))}
            />
          </div>

          <div className="flex items-center gap-3">
            <button
              className="px-4 py-2 bg-violet-700 hover:bg-violet-600
                         text-white text-sm rounded transition-colors"
              onClick={() => setServer(form)}
            >
              Save
            </button>
            <StatusDot
              status={connected ? "connected" : "disconnected"}
              label={connected ? "Connected" : "Not connected"}
            />
          </div>
        </section>
      </div>
    </div>
  )
}
```

---

## 14. Navigation Layout

```typescript
// app/layout.tsx

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="en" className="dark">
      <body className="bg-gray-950 text-gray-100 antialiased">
        <div className="flex h-screen">
          {/* Sidebar navigation */}
          <nav className="w-14 border-r border-gray-800 flex flex-col
                          items-center py-4 gap-4">
            <NavIcon href="/query"   icon={<Terminal />}  label="Query" />
            <NavIcon href="/graph"   icon={<Network />}   label="Graph" />
            <NavIcon href="/history" icon={<History />}   label="History" />
            <div className="flex-1" />
            <NavIcon href="/settings" icon={<Settings />} label="Settings" />
          </nav>

          {/* Page content */}
          <main className="flex-1 overflow-hidden">
            {children}
          </main>
        </div>
      </body>
    </html>
  )
}
```

---

## 15. Server Health Hook

```typescript
// hooks/useServerStatus.ts

export function useServerStatus() {
  const { server, setConnected } = useStore()
  const [connected, setLocalConnected] = useState(false)

  useEffect(() => {
    let cancelled = false

    async function check() {
      try {
        const client = new CEApiClient(server.baseURL, server.token)
        await client.health()
        if (!cancelled) {
          setLocalConnected(true)
          setConnected(true)
        }
      } catch {
        if (!cancelled) {
          setLocalConnected(false)
          setConnected(false)
        }
      }
    }

    check()

    // Poll every 10 seconds
    const interval = setInterval(check, 10_000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [server.baseURL, server.token])

  return { connected }
}
```

---

## 16. Key Design Decisions

**Dark theme throughout** — violet/gray palette. The TUI uses the same color
language so the two interfaces feel like one product. Code blocks on dark
backgrounds read better for developers.

**No loading spinners on graph** — the graph shows whatever nodes are loaded.
Pagination is triggered by scrolling the controls sidebar, not waiting for
all nodes. Large graphs render incrementally.

**Trace inspector is read-only** — no editing or re-running from the inspector.
The inspector is forensic. New queries go through the query interface.

**WebSocket for live queries, REST for everything else** — don't stream
historical data over WebSocket. History, substrate, and exec log all use
REST with pagination.

**Cytoscape fcose layout** — the force-directed layout handles graphs up to
~5,000 nodes comfortably in a browser. Beyond that, the activation filter
becomes essential — filtering to `min_activation > 0.1` typically reduces
any graph to the most relevant few hundred nodes.

---

## 17. Package Layout Summary

```
ce-studio/ (separate repository)
  app/
    layout.tsx
    query/page.tsx
    query/[runId]/page.tsx
    graph/page.tsx
    history/page.tsx
    settings/page.tsx
  components/
    query/   QueryInput, StreamView, AnswerView, LoopProgress, ToolIndicators
    trace/   TraceTimeline, LLMCallCard, StrategyCard, ReviewCard, SynthCard
    graph/   GraphCanvas, NodePanel, EdgePanel, GraphControls
    shared/  CodeBlock, MarkdownView, CostBadge, StatusDot, NavIcon
  lib/
    api.ts   types.ts   store.ts   format.ts
  hooks/
    useQuery.ts   useExecLog.ts   useSubstrate.ts   useServerStatus.ts
  public/
  next.config.ts
  tailwind.config.ts
  tsconfig.json
  package.json
```

---

## 18. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| Framework | Next.js 14 App Router |
| Styling | Tailwind CSS + shadcn/ui |
| Graph rendering | Cytoscape.js with fcose layout |
| State | Zustand with localStorage persistence for server config |
| Streaming | Native WebSocket, custom hook |
| Markdown | react-markdown + remark-gfm |
| Code highlighting | Shiki |
| Theme | Dark (gray-950 base, violet accent) |
| Navigation | Icon sidebar (4 routes) |
| Query page | Stream view → answer view transition |
| Trace page | Timeline sidebar + main call detail |
| Graph page | Controls sidebar + full-canvas Cytoscape |
| History page | Paginated run list, click to open trace |
| Server config | Settings page, persisted in localStorage via Zustand |
| Local auth | No token required for localhost connections |
| Graph performance | Activation filter reduces visible nodes for large graphs |

---

*Spec 15: CE Studio — v1.0 — February 2026*
*Next: Spec 16 — Default Plugins (Go, TypeScript, Python)*
*Companion: Context Engine PRD v0.5 Section 16 | Decisions Log v1.0 Section 11*
