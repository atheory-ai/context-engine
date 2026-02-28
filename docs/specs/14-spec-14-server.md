# Context Engine — Spec 14: MCP + API Server
## Implementation Spec — MCP Protocol, REST API, WebSocket, Server Process
### Version 1.0 | February 2026

---

> This spec covers the server layer — the MCP server for IDE integration
> and the REST/WebSocket API for CE Studio and external consumers.
> Hand to Claude Code alongside spec-2-packages.md and spec-6-cli-config.md.
> Companion: Context Engine PRD v0.5 Section 14. Decisions Log v1.0 Section 10.

---

## 1. Overview

The `ce server` process runs two servers in the same binary:

1. **MCP Server** — Model Context Protocol, two transports:
   - `stdio` — subprocess transport for Claude Desktop, Cursor, Claude Code
   - `SSE` — HTTP server-sent events for remote/browser MCP clients

2. **API Server** — REST + WebSocket for CE Studio and external consumers:
   - REST endpoints for project status, substrate queries, execution log
   - WebSocket for real-time query streaming to CE Studio

Both servers share the same engine instance. A query running via MCP
and a query running via the API use the same cognitive loop, same substrate,
same channel system.

---

## 2. Package Structure

```
internal/server/
  server.go           — Server struct, Start(), Stop(), lifecycle
  mcp/
    mcp.go            — MCP server, tool registration
    stdio.go          — stdio transport handler
    sse.go            — SSE transport handler
    tools/
      query.go        — ce_query MCP tool
      index.go        — ce_index MCP tool
      status.go       — ce_status MCP tool
      search.go       — ce_search MCP tool
    protocol/
      types.go        — MCP protocol types (JSON-RPC 2.0)
      marshal.go      — request/response marshaling
  api/
    api.go            — HTTP router, middleware
    auth.go           — token authentication middleware
    handlers/
      query.go        — POST /api/v1/query
      projects.go     — GET/POST /api/v1/projects
      substrate.go    — GET /api/v1/substrate/*
      execlog.go      — GET /api/v1/execlog/*
      tokens.go       — POST/GET/DELETE /api/v1/tokens
      health.go       — GET /health
    ws/
      ws.go           — WebSocket upgrade, hub
      stream.go       — query stream → WebSocket frames
```

---

## 3. Server Lifecycle

```go
// internal/server/server.go

package server

// Server manages both the MCP server and API server.
type Server struct {
    cfg      *config.Config
    engine   *runner.Engine
    mcp      *mcp.Server
    api      *api.Server
    stopCh   chan struct{}
    wg       sync.WaitGroup
}

func New(cfg *config.Config, engine *runner.Engine) *Server {
    return &Server{
        cfg:    cfg,
        engine: engine,
        stopCh: make(chan struct{}),
    }
}

// Start launches both servers. Blocks until Stop() is called.
func (s *Server) Start(ctx context.Context) error {
    // API server (REST + WebSocket)
    if s.cfg.Server.APIEnabled || s.cfg.Server.WSEnabled {
        s.api = api.New(s.cfg, s.engine)
        s.wg.Add(1)
        go func() {
            defer s.wg.Done()
            addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
            if err := s.api.Start(ctx, addr); err != nil {
                if !errors.Is(err, http.ErrServerClosed) {
                    log.Printf("API server error: %v", err)
                }
            }
        }()
    }

    // MCP SSE server (same port, different path prefix)
    if s.cfg.Server.MCPEnabled {
        s.mcp = mcp.New(s.cfg, s.engine)
        s.wg.Add(1)
        go func() {
            defer s.wg.Done()
            addr := fmt.Sprintf("%s:%d", s.cfg.Server.Host, s.cfg.Server.Port)
            if err := s.mcp.StartSSE(ctx, addr); err != nil {
                if !errors.Is(err, http.ErrServerClosed) {
                    log.Printf("MCP SSE server error: %v", err)
                }
            }
        }()
    }

    // Write PID file for ce server stop
    s.writePIDFile()

    // Block until context cancelled or stop signal
    select {
    case <-ctx.Done():
    case <-s.stopCh:
    }

    s.shutdown()
    s.wg.Wait()
    return nil
}

func (s *Server) Stop() {
    close(s.stopCh)
}

func (s *Server) shutdown() {
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if s.api != nil {
        s.api.Shutdown(ctx)
    }
    if s.mcp != nil {
        s.mcp.Shutdown(ctx)
    }
    s.removePIDFile()
}

func (s *Server) writePIDFile() {
    pidPath := filepath.Join(s.cfg.DataDir, "server.pid")
    os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0644)
}

func (s *Server) removePIDFile() {
    os.Remove(filepath.Join(s.cfg.DataDir, "server.pid"))
}
```

### ce server start/stop/status (CLI amendment)

```go
// cli/server.go (amended)

func runServerStart(cmd *cobra.Command, args []string) error {
    cfg, err := config.Load()
    if err != nil {
        return err
    }

    ctx, cancel := signal.NotifyContext(context.Background(),
        os.Interrupt, syscall.SIGTERM)
    defer cancel()

    engine, err := runner.New(ctx, cfg)
    if err != nil {
        return fmt.Errorf("engine init: %w", err)
    }
    defer engine.Close(context.Background())

    srv := server.New(cfg, engine)

    addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
    fmt.Printf("CE server running at %s\n", addr)
    if cfg.Server.MCPEnabled {
        fmt.Printf("MCP SSE endpoint: http://%s/mcp/sse\n", addr)
    }
    fmt.Printf("API endpoint:     http://%s/api/v1\n", addr)
    fmt.Println()
    fmt.Println("ctrl+c to stop")

    return srv.Start(ctx)
}

func runServerStop(cmd *cobra.Command, args []string) error {
    cfg, err := config.Load()
    if err != nil {
        return err
    }

    pidPath := filepath.Join(cfg.DataDir, "server.pid")
    data, err := os.ReadFile(pidPath)
    if err != nil {
        return fmt.Errorf("server is not running (no PID file)")
    }

    pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
    if err != nil {
        return fmt.Errorf("invalid PID file: %w", err)
    }

    proc, err := os.FindProcess(pid)
    if err != nil {
        return fmt.Errorf("process not found: %w", err)
    }

    if err := proc.Signal(os.Interrupt); err != nil {
        return fmt.Errorf("send stop signal: %w", err)
    }

    fmt.Printf("Sent stop signal to CE server (PID %d)\n", pid)
    return nil
}

func runServerStatus(cmd *cobra.Command, args []string) error {
    cfg, err := config.Load()
    if err != nil {
        return err
    }

    pidPath := filepath.Join(cfg.DataDir, "server.pid")
    data, err := os.ReadFile(pidPath)
    if err != nil {
        fmt.Println("CE server: not running")
        return nil
    }

    pid, _ := strconv.Atoi(strings.TrimSpace(string(data)))

    // Check if process is actually alive
    proc, err := os.FindProcess(pid)
    if err != nil || proc.Signal(syscall.Signal(0)) != nil {
        fmt.Println("CE server: not running (stale PID file)")
        os.Remove(pidPath)
        return nil
    }

    addr := fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
    fmt.Printf("CE server: running (PID %d)\n", pid)
    fmt.Printf("  Address: http://%s\n", addr)
    if cfg.Server.MCPEnabled {
        fmt.Printf("  MCP SSE: http://%s/mcp/sse\n", addr)
    }
    return nil
}
```

---

## 4. MCP Protocol Types

MCP uses JSON-RPC 2.0. These are the types we need.

```go
// internal/server/mcp/protocol/types.go

package protocol

// JSON-RPC 2.0 base types

type Request struct {
    JSONRPC string          `json:"jsonrpc"` // "2.0"
    ID      any             `json:"id"`      // string | number | null
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
    JSONRPC string          `json:"jsonrpc"` // "2.0"
    ID      any             `json:"id"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *RPCError       `json:"error,omitempty"`
}

type RPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
    Data    any    `json:"data,omitempty"`
}

type Notification struct {
    JSONRPC string          `json:"jsonrpc"` // "2.0"
    Method  string          `json:"method"`
    Params  json.RawMessage `json:"params,omitempty"`
}

// MCP-specific types

// InitializeParams — sent by client on connection
type InitializeParams struct {
    ProtocolVersion string         `json:"protocolVersion"`
    Capabilities    ClientCapabilities `json:"capabilities"`
    ClientInfo      ClientInfo     `json:"clientInfo"`
}

type ClientCapabilities struct {
    Roots   *RootsCapability   `json:"roots,omitempty"`
    Sampling *SamplingCapability `json:"sampling,omitempty"`
}

type RootsCapability struct {
    ListChanged bool `json:"listChanged"`
}

type SamplingCapability struct{}

type ClientInfo struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}

// InitializeResult — server response to initialize
type InitializeResult struct {
    ProtocolVersion string             `json:"protocolVersion"`
    Capabilities    ServerCapabilities `json:"capabilities"`
    ServerInfo      ServerInfo         `json:"serverInfo"`
}

type ServerCapabilities struct {
    Tools *ToolsCapability `json:"tools,omitempty"`
}

type ToolsCapability struct {
    ListChanged bool `json:"listChanged"`
}

type ServerInfo struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}

// Tool definition for tools/list response
type Tool struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema"` // JSON Schema
}

// CallToolParams — tools/call request
type CallToolParams struct {
    Name      string          `json:"name"`
    Arguments json.RawMessage `json:"arguments"`
}

// CallToolResult — tools/call response
type CallToolResult struct {
    Content []ToolContent `json:"content"`
    IsError bool          `json:"isError,omitempty"`
}

type ToolContent struct {
    Type string `json:"type"` // "text" | "image" | "resource"
    Text string `json:"text,omitempty"`
}
```

---

## 5. MCP Server

```go
// internal/server/mcp/mcp.go

package mcp

const MCPProtocolVersion = "2024-11-05"

// Server handles MCP protocol over both stdio and SSE transports.
type Server struct {
    cfg    *config.Config
    engine *runner.Engine
    tools  []protocol.Tool
    handlers map[string]toolHandler
}

type toolHandler func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error)

func New(cfg *config.Config, engine *runner.Engine) *Server {
    s := &Server{
        cfg:      cfg,
        engine:   engine,
        handlers: make(map[string]toolHandler),
    }
    s.registerTools()
    return s
}

func (s *Server) registerTools() {
    // Register all four CE tools
    tools.RegisterQuery(s)
    tools.RegisterIndex(s)
    tools.RegisterStatus(s)
    tools.RegisterSearch(s)
}

// RegisterTool adds a tool to the server.
// Called by each tool's Register() function.
func (s *Server) RegisterTool(tool protocol.Tool, handler toolHandler) {
    s.tools = append(s.tools, tool)
    s.handlers[tool.Name] = handler
}

// handleRequest routes a JSON-RPC request to the appropriate handler.
func (s *Server) handleRequest(ctx context.Context, req protocol.Request) protocol.Response {
    switch req.Method {

    case "initialize":
        return s.handleInitialize(req)

    case "tools/list":
        return s.handleToolsList(req)

    case "tools/call":
        return s.handleToolsCall(ctx, req)

    case "ping":
        return protocol.Response{
            JSONRPC: "2.0",
            ID:      req.ID,
            Result:  json.RawMessage(`{}`),
        }

    default:
        return protocol.ErrorResponse(req.ID,
            -32601, "method not found", req.Method)
    }
}

func (s *Server) handleInitialize(req protocol.Request) protocol.Response {
    result := protocol.InitializeResult{
        ProtocolVersion: MCPProtocolVersion,
        Capabilities: protocol.ServerCapabilities{
            Tools: &protocol.ToolsCapability{ListChanged: false},
        },
        ServerInfo: protocol.ServerInfo{
            Name:    "context-engine",
            Version: version.String(),
        },
    }
    return protocol.OKResponse(req.ID, result)
}

func (s *Server) handleToolsList(req protocol.Request) protocol.Response {
    type listResult struct {
        Tools []protocol.Tool `json:"tools"`
    }
    return protocol.OKResponse(req.ID, listResult{Tools: s.tools})
}

func (s *Server) handleToolsCall(ctx context.Context, req protocol.Request) protocol.Response {
    var params protocol.CallToolParams
    if err := json.Unmarshal(req.Params, &params); err != nil {
        return protocol.ErrorResponse(req.ID, -32602, "invalid params", err.Error())
    }

    handler, ok := s.handlers[params.Name]
    if !ok {
        return protocol.ErrorResponse(req.ID, -32602,
            "unknown tool", params.Name)
    }

    result, err := handler(ctx, params.Arguments)
    if err != nil {
        result = protocol.CallToolResult{
            Content: []protocol.ToolContent{{Type: "text", Text: err.Error()}},
            IsError: true,
        }
    }

    return protocol.OKResponse(req.ID, result)
}
```

---

## 6. MCP Stdio Transport

```go
// internal/server/mcp/stdio.go

package mcp

// RunStdio runs the MCP server over stdio transport.
// Used by Claude Desktop, Cursor, Claude Code.
// Called directly — does not start a network server.
func (s *Server) RunStdio(ctx context.Context) error {
    scanner := bufio.NewScanner(os.Stdin)
    encoder := json.NewEncoder(os.Stdout)

    // Larger buffer for big responses
    buf := make([]byte, 0, 64*1024)
    scanner.Buffer(buf, 1024*1024)

    for scanner.Scan() {
        line := scanner.Bytes()
        if len(line) == 0 {
            continue
        }

        // Check context cancellation
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        var req protocol.Request
        if err := json.Unmarshal(line, &req); err != nil {
            // Write parse error
            encoder.Encode(protocol.ErrorResponse(nil, -32700, "parse error", err.Error()))
            continue
        }

        // Handle notification (no response needed)
        if req.ID == nil && req.Method != "" {
            s.handleNotification(ctx, req)
            continue
        }

        resp := s.handleRequest(ctx, req)
        if err := encoder.Encode(resp); err != nil {
            return fmt.Errorf("write response: %w", err)
        }
    }

    if err := scanner.Err(); err != nil {
        return fmt.Errorf("stdin read: %w", err)
    }

    return nil
}

func (s *Server) handleNotification(ctx context.Context, req protocol.Request) {
    switch req.Method {
    case "notifications/initialized":
        // Client confirmed initialization — nothing to do
    case "notifications/cancelled":
        // Client cancelled a request — context cancellation handles this
    }
}
```

### ce mcp-stdio subcommand

```go
// cli/server.go (addition)

// ce mcp-stdio — run MCP server over stdio for IDE integration
// This is what Claude Desktop / Cursor / Claude Code configures
var mcpStdioCmd = &cobra.Command{
    Use:    "mcp-stdio",
    Short:  "Run MCP server over stdio (for IDE integration)",
    Hidden: true, // Not shown in help — IDE tools call this directly
    RunE: func(cmd *cobra.Command, args []string) error {
        cfg, err := config.Load()
        if err != nil {
            return err
        }

        ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
        defer cancel()

        engine, err := runner.New(ctx, cfg)
        if err != nil {
            return fmt.Errorf("engine init: %w", err)
        }
        defer engine.Close(context.Background())

        mcpServer := mcp.New(cfg, engine)
        return mcpServer.RunStdio(ctx)
    },
}
```

### Claude Desktop / Cursor configuration

Users add CE to their MCP config:

```json
// ~/.config/claude/claude_desktop_config.json
{
  "mcpServers": {
    "context-engine": {
      "command": "ce",
      "args": ["mcp-stdio"],
      "env": {
        "ANTHROPIC_API_KEY": "..."
      }
    }
  }
}
```

---

## 7. MCP SSE Transport

```go
// internal/server/mcp/sse.go

package mcp

// StartSSE starts the MCP server over SSE transport.
// SSE path: /mcp/sse
// Messages path: /mcp/messages
func (s *Server) StartSSE(ctx context.Context, addr string) error {
    mux := http.NewServeMux()

    // SSE connection endpoint — client connects here first
    mux.HandleFunc("/mcp/sse", s.handleSSEConnect)

    // Message endpoint — client POSTs JSON-RPC requests here
    mux.HandleFunc("/mcp/messages", s.handleSSEMessage)

    srv := &http.Server{
        Addr:    addr,
        Handler: mux,
    }

    go func() {
        <-ctx.Done()
        srv.Shutdown(context.Background())
    }()

    if err := srv.ListenAndServe(); err != http.ErrServerClosed {
        return err
    }
    return nil
}

// SSE session tracks an active SSE connection.
type sseSession struct {
    id      string
    writer  http.ResponseWriter
    flusher http.Flusher
    sendCh  chan string  // JSON responses to send
    done    chan struct{}
}

var sessions sync.Map // sessionID → *sseSession

func (s *Server) handleSSEConnect(w http.ResponseWriter, r *http.Request) {
    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "SSE not supported", http.StatusInternalServerError)
        return
    }

    sessionID := uuid.New().String()

    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")
    w.Header().Set("Access-Control-Allow-Origin", "*")

    session := &sseSession{
        id:      sessionID,
        writer:  w,
        flusher: flusher,
        sendCh:  make(chan string, 32),
        done:    make(chan struct{}),
    }
    sessions.Store(sessionID, session)
    defer sessions.Delete(sessionID)

    // Send endpoint URL so client knows where to POST messages
    fmt.Fprintf(w, "event: endpoint\n")
    fmt.Fprintf(w, "data: /mcp/messages?sessionId=%s\n\n", sessionID)
    flusher.Flush()

    // Forward responses to client
    for {
        select {
        case msg := <-session.sendCh:
            fmt.Fprintf(w, "event: message\n")
            fmt.Fprintf(w, "data: %s\n\n", msg)
            flusher.Flush()
        case <-session.done:
            return
        case <-r.Context().Done():
            return
        }
    }
}

func (s *Server) handleSSEMessage(w http.ResponseWriter, r *http.Request) {
    sessionID := r.URL.Query().Get("sessionId")
    val, ok := sessions.Load(sessionID)
    if !ok {
        http.Error(w, "session not found", http.StatusNotFound)
        return
    }
    session := val.(*sseSession)

    var req protocol.Request
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "invalid JSON-RPC", http.StatusBadRequest)
        return
    }

    // Handle notification (no response)
    if req.ID == nil {
        s.handleNotification(r.Context(), req)
        w.WriteHeader(http.StatusAccepted)
        return
    }

    resp := s.handleRequest(r.Context(), req)

    data, _ := json.Marshal(resp)
    session.sendCh <- string(data)

    w.WriteHeader(http.StatusAccepted)
}
```

---

## 8. MCP Tools

### ce_query

```go
// internal/server/mcp/tools/query.go

var queryTool = protocol.Tool{
    Name: "ce_query",
    Description: `Run an AI-powered investigation query against the indexed codebase.
Returns a structured answer with specific code references.
Use for: understanding how code works, tracing call chains, finding where concepts are implemented.`,
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "query": {
                "type": "string",
                "description": "The investigation question. Be specific about what you want to understand."
            },
            "project": {
                "type": "string",
                "description": "Git remote URL of the project to query. Omit to use the active project."
            },
            "max_loops": {
                "type": "integer",
                "description": "Maximum cognitive loop iterations (1-10). Default: project setting.",
                "minimum": 1,
                "maximum": 10
            }
        },
        "required": ["query"]
    }`),
}

func RegisterQuery(s *mcp.Server) {
    s.RegisterTool(queryTool, handleQuery(s.Engine()))
}

func handleQuery(engine *runner.Engine) toolHandler {
    return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
        var params struct {
            Query    string `json:"query"`
            Project  string `json:"project"`
            MaxLoops int    `json:"max_loops"`
        }
        if err := json.Unmarshal(args, &params); err != nil {
            return protocol.CallToolResult{}, fmt.Errorf("invalid params: %w", err)
        }

        if params.Query == "" {
            return protocol.CallToolResult{}, fmt.Errorf("query is required")
        }

        // Run query synchronously — MCP waits for result
        result, err := engine.QuerySync(ctx, runner.QueryOptions{
            Query:    params.Query,
            MaxLoops: params.MaxLoops,
        })
        if err != nil {
            return protocol.CallToolResult{
                Content: []protocol.ToolContent{{
                    Type: "text",
                    Text: fmt.Sprintf("Query failed: %v", err),
                }},
                IsError: true,
            }, nil
        }

        return protocol.CallToolResult{
            Content: []protocol.ToolContent{{
                Type: "text",
                Text: result.Answer,
            }},
        }, nil
    }
}
```

### ce_index

```go
// internal/server/mcp/tools/index.go

var indexTool = protocol.Tool{
    Name: "ce_index",
    Description: `Trigger reindexing of the project codebase.
Use when: files have changed and you want CE to pick up the changes before querying.
Returns indexing statistics when complete.`,
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "project": {
                "type": "string",
                "description": "Git remote URL of the project to index. Omit for active project."
            },
            "full": {
                "type": "boolean",
                "description": "Force full reindex (ignore file hashes). Default: false (incremental)."
            }
        }
    }`),
}
```

### ce_status

```go
// internal/server/mcp/tools/status.go

var statusTool = protocol.Tool{
    Name: "ce_status",
    Description: `Get the current index status for a project.
Returns: node count, edge count, last indexed time, index completeness.
Use before querying to verify the project is indexed.`,
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "project": {
                "type": "string",
                "description": "Git remote URL. Omit for active project."
            }
        }
    }`),
}

func handleStatus(engine *runner.Engine) toolHandler {
    return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
        status, err := engine.ProjectStatus(ctx)
        if err != nil {
            return protocol.CallToolResult{}, err
        }

        text := fmt.Sprintf(`Project: %s
Status: %s
Nodes: %d
Edges: %d
Files indexed: %d
Last indexed: %s`,
            status.GitURL,
            status.IndexState,
            status.NodeCount,
            status.EdgeCount,
            status.FilesIndexed,
            status.LastIndexed.Format(time.RFC3339),
        )

        return protocol.CallToolResult{
            Content: []protocol.ToolContent{{Type: "text", Text: text}},
        }, nil
    }
}
```

### ce_search

```go
// internal/server/mcp/tools/search.go

var searchTool = protocol.Tool{
    Name: "ce_search",
    Description: `Lightweight substrate search without running the full cognitive loop.
Returns matching nodes directly from the knowledge graph.
Use for: looking up specific symbols, finding files by name, checking if something is indexed.
Faster than ce_query but less intelligent — no activation propagation or tool fan-out.`,
    InputSchema: json.RawMessage(`{
        "type": "object",
        "properties": {
            "query": {
                "type": "string",
                "description": "Search term. Matches against node labels and canonical IDs."
            },
            "type": {
                "type": "string",
                "description": "Filter by node type: symbol, namespace, concept, file",
                "enum": ["symbol", "namespace", "concept", "file"]
            },
            "limit": {
                "type": "integer",
                "description": "Maximum results (default: 10, max: 50)",
                "minimum": 1,
                "maximum": 50
            }
        },
        "required": ["query"]
    }`),
}

func handleSearch(engine *runner.Engine) toolHandler {
    return func(ctx context.Context, args json.RawMessage) (protocol.CallToolResult, error) {
        var params struct {
            Query string `json:"query"`
            Type  string `json:"type"`
            Limit int    `json:"limit"`
        }
        if err := json.Unmarshal(args, &params); err != nil {
            return protocol.CallToolResult{}, err
        }

        limit := params.Limit
        if limit == 0 {
            limit = 10
        }

        nodes, err := engine.SearchSubstrate(ctx, runner.SearchOptions{
            Query: params.Query,
            Type:  params.Type,
            Limit: limit,
        })
        if err != nil {
            return protocol.CallToolResult{}, err
        }

        if len(nodes) == 0 {
            return protocol.CallToolResult{
                Content: []protocol.ToolContent{{
                    Type: "text",
                    Text: fmt.Sprintf("No nodes found matching %q", params.Query),
                }},
            }, nil
        }

        var lines []string
        for _, node := range nodes {
            lines = append(lines, fmt.Sprintf("[%s] %s (%s)",
                node.Type, node.CanonicalID, node.SourceClass))
        }

        return protocol.CallToolResult{
            Content: []protocol.ToolContent{{
                Type: "text",
                Text: strings.Join(lines, "\n"),
            }},
        }, nil
    }
}
```

---

## 9. API Server

```go
// internal/server/api/api.go

package api

// Server is the REST + WebSocket API server.
type Server struct {
    cfg    *config.Config
    engine *runner.Engine
    srv    *http.Server
}

func New(cfg *config.Config, engine *runner.Engine) *Server {
    s := &Server{cfg: cfg, engine: engine}

    mux := http.NewServeMux()

    // Health
    mux.HandleFunc("GET /health", handlers.Health)

    // Auth middleware applied to all /api/ routes
    apiMux := http.NewServeMux()
    apiMux.HandleFunc("GET  /api/v1/projects",          handlers.ListProjects(engine))
    apiMux.HandleFunc("GET  /api/v1/projects/{id}",     handlers.GetProject(engine))
    apiMux.HandleFunc("POST /api/v1/query",             handlers.Query(engine))
    apiMux.HandleFunc("GET  /api/v1/substrate/nodes",   handlers.GetNodes(engine))
    apiMux.HandleFunc("GET  /api/v1/substrate/edges",   handlers.GetEdges(engine))
    apiMux.HandleFunc("GET  /api/v1/execlog",           handlers.ListExecLog(engine))
    apiMux.HandleFunc("GET  /api/v1/execlog/{runId}",   handlers.GetExecRun(engine))
    apiMux.HandleFunc("POST /api/v1/tokens",            handlers.CreateToken(engine))
    apiMux.HandleFunc("GET  /api/v1/tokens",            handlers.ListTokens(engine))
    apiMux.HandleFunc("DELETE /api/v1/tokens/{id}",     handlers.RevokeToken(engine))

    // WebSocket for streaming queries
    apiMux.HandleFunc("GET  /api/v1/ws",                ws.Handler(engine))

    // Wrap with auth middleware
    mux.Handle("/api/", auth.Middleware(cfg)(apiMux))

    s.srv = &http.Server{
        Addr:         "",  // set in Start()
        Handler:      corsMiddleware(cfg)(mux),
        ReadTimeout:  30 * time.Second,
        WriteTimeout: 120 * time.Second,
        IdleTimeout:  120 * time.Second,
    }

    return s
}

func (s *Server) Start(ctx context.Context, addr string) error {
    s.srv.Addr = addr

    go func() {
        <-ctx.Done()
        s.srv.Shutdown(context.Background())
    }()

    return s.srv.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
    return s.srv.Shutdown(ctx)
}
```

---

## 10. Token Authentication Middleware

```go
// internal/server/api/auth.go

package auth

// Middleware validates CE API tokens on all /api/ requests.
// Tokens are created via `ce token create` and stored in audit.db.
func Middleware(cfg *config.Config) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Extract token from header or query param
            token := extractToken(r)

            // Local requests (127.0.0.1 with no token) are allowed
            // when server is in local-only mode
            if token == "" && isLocalRequest(r) && cfg.Server.Host == "127.0.0.1" {
                next.ServeHTTP(w, r)
                return
            }

            if token == "" {
                writeUnauthorized(w, "missing token")
                return
            }

            // Validate token against audit.db
            tokenRecord, err := validateToken(cfg.DataDir, token)
            if err != nil || tokenRecord == nil {
                writeUnauthorized(w, "invalid token")
                return
            }

            // Check expiry
            if tokenRecord.ExpiresAt != nil {
                if time.Now().UnixMilli() > *tokenRecord.ExpiresAt {
                    writeUnauthorized(w, "token expired")
                    return
                }
            }

            // Check write operations against read-only scope
            if tokenRecord.Scope == "read" && isWriteMethod(r.Method) {
                writeForbidden(w, "read-only token cannot perform write operations")
                return
            }

            // Attach token to context for handlers
            ctx := context.WithValue(r.Context(), tokenContextKey, tokenRecord)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func extractToken(r *http.Request) string {
    // Bearer token in Authorization header
    auth := r.Header.Get("Authorization")
    if strings.HasPrefix(auth, "Bearer ") {
        return strings.TrimPrefix(auth, "Bearer ")
    }
    // Fall back to CE-Token header (convenience for WebSocket)
    if t := r.Header.Get("CE-Token"); t != "" {
        return t
    }
    // Query param (for WebSocket where headers are hard)
    return r.URL.Query().Get("token")
}

func isLocalRequest(r *http.Request) bool {
    host, _, err := net.SplitHostPort(r.RemoteAddr)
    if err != nil {
        return false
    }
    return host == "127.0.0.1" || host == "::1"
}
```

---

## 11. REST API Handlers

### POST /api/v1/query

```go
// internal/server/api/handlers/query.go

type QueryRequest struct {
    Query    string `json:"query"`
    MaxLoops int    `json:"max_loops,omitempty"`
    Stream   bool   `json:"stream,omitempty"` // if true, use WebSocket instead
}

type QueryResponse struct {
    RunID     string    `json:"run_id"`
    Answer    string    `json:"answer"`
    TokensIn  int       `json:"tokens_in"`
    TokensOut int       `json:"tokens_out"`
    CostUSD   float64   `json:"cost_usd"`
    LoopsUsed int       `json:"loops_used"`
    DurationMS int64    `json:"duration_ms"`
    Partial   bool      `json:"partial"`
}

func Query(engine *runner.Engine) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        var req QueryRequest
        if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
            writeError(w, http.StatusBadRequest, "invalid request body")
            return
        }

        if req.Query == "" {
            writeError(w, http.StatusBadRequest, "query is required")
            return
        }

        result, err := engine.QuerySync(r.Context(), runner.QueryOptions{
            Query:    req.Query,
            MaxLoops: req.MaxLoops,
        })
        if err != nil {
            writeError(w, http.StatusInternalServerError, err.Error())
            return
        }

        writeJSON(w, http.StatusOK, QueryResponse{
            RunID:      result.RunID,
            Answer:     result.Answer,
            TokensIn:   result.TokensIn,
            TokensOut:  result.TokensOut,
            CostUSD:    result.CostUSD,
            LoopsUsed:  result.LoopsUsed,
            DurationMS: result.DurationMS,
            Partial:    result.Partial,
        })
    }
}
```

### GET /api/v1/execlog

```go
// internal/server/api/handlers/execlog.go

// GET /api/v1/execlog — list recent query runs
// Query params: limit (default 20), offset (default 0), project_id

// GET /api/v1/execlog/{runId} — get full execution trace for a run
// Returns: run metadata + all LLM calls with prompts and responses
// Used by CE Studio to render the cognitive trace visualization

type ExecRunDetail struct {
    RunID       string       `json:"run_id"`
    Query       string       `json:"query"`
    ProjectID   string       `json:"project_id"`
    StartedAt   int64        `json:"started_at"`
    CompletedAt *int64       `json:"completed_at"`
    LoopsUsed   int          `json:"loops_used"`
    TokensIn    int          `json:"tokens_in"`
    TokensOut   int          `json:"tokens_out"`
    CostUSD     float64      `json:"cost_usd"`
    Partial     bool         `json:"partial"`
    LLMCalls    []LLMCallLog `json:"llm_calls"`
}

type LLMCallLog struct {
    CallID      string  `json:"call_id"`
    NodeType    string  `json:"node_type"`  // "strategizer" | "reviewer" | "synthesizer"
    LoopIndex   int     `json:"loop_index"`
    Model       string  `json:"model"`
    SystemPrompt string `json:"system_prompt"`
    UserMessage  string `json:"user_message"`
    Response     string `json:"response"`
    ThinkingText string `json:"thinking_text,omitempty"`
    TokensIn    int     `json:"tokens_in"`
    TokensOut   int     `json:"tokens_out"`
    LatencyMS   int64   `json:"latency_ms"`
    CalledAt    int64   `json:"called_at"`
}
```

### GET /api/v1/substrate/nodes

```go
// GET /api/v1/substrate/nodes
// Query params:
//   project_id (required)
//   type (optional: symbol|namespace|concept|file)
//   search (optional: substring match on label/canonical_id)
//   limit (default 50, max 200)
//   offset (default 0)
//   min_activation (optional: float, filter by activation level)

type NodesResponse struct {
    Nodes  []NodeResponse `json:"nodes"`
    Total  int            `json:"total"`
    Offset int            `json:"offset"`
}

type NodeResponse struct {
    ID           string         `json:"id"`
    Type         string         `json:"type"`
    Label        string         `json:"label"`
    CanonicalID  string         `json:"canonical_id"`
    SourceClass  string         `json:"source_class"`
    Activation   float64        `json:"activation"`
    PeakActivation float64      `json:"peak_activation"`
    Properties   map[string]any `json:"properties"`
}
```

---

## 12. WebSocket Streaming

```go
// internal/server/api/ws/ws.go

// GET /api/v1/ws — WebSocket upgrade for streaming queries
// After upgrade, client sends QueryRequest JSON.
// Server streams events as JSON frames until query completes.

type WSEvent struct {
    Type    string `json:"type"`    // "thinking" | "action" | "message" | "warning" | "error" | "cost" | "done"
    Content string `json:"content"`
    Metadata map[string]any `json:"metadata,omitempty"`
}

func Handler(engine *runner.Engine) http.HandlerFunc {
    upgrader := websocket.Upgrader{
        CheckOrigin: func(r *http.Request) bool {
            return checkOrigin(r)  // validates against cfg.Server.CORSOrigins
        },
    }

    return func(w http.ResponseWriter, r *http.Request) {
        conn, err := upgrader.Upgrade(w, r, nil)
        if err != nil {
            return
        }
        defer conn.Close()

        // Read query from client
        var req QueryRequest
        if err := conn.ReadJSON(&req); err != nil {
            conn.WriteJSON(WSEvent{Type: "error", Content: "invalid request"})
            return
        }

        // Create a dedicated channel set for this WebSocket connection
        ch := engine.NewChannels()
        defer engine.CloseChannels(ch)

        // Run query in background
        queryErr := make(chan error, 1)
        go func() {
            queryErr <- engine.QueryWithChannels(r.Context(), req.Query, ch,
                runner.QueryOptions{MaxLoops: req.MaxLoops})
        }()

        // Stream channel events to WebSocket
        for {
            select {
            case e := <-ch.Thinking:
                conn.WriteJSON(WSEvent{Type: "thinking", Content: e.Content, Metadata: e.Metadata})
            case e := <-ch.Action:
                conn.WriteJSON(WSEvent{Type: "action", Content: e.Content})
            case e := <-ch.Message:
                conn.WriteJSON(WSEvent{Type: "message", Content: e.Content})
            case e := <-ch.Warning:
                conn.WriteJSON(WSEvent{Type: "warning", Content: e.Content})
            case e := <-ch.Error:
                conn.WriteJSON(WSEvent{Type: "error", Content: e.Content})
            case e := <-ch.Cost:
                conn.WriteJSON(WSEvent{Type: "cost", Content: e.Content, Metadata: e.Metadata})
            case err := <-queryErr:
                if err != nil {
                    conn.WriteJSON(WSEvent{Type: "error", Content: err.Error()})
                }
                conn.WriteJSON(WSEvent{Type: "done"})
                return
            case <-r.Context().Done():
                return
            }
        }
    }
}
```

---

## 13. CORS Middleware

```go
// internal/server/api/api.go (cors)

func corsMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            origin := r.Header.Get("Origin")

            if isAllowedOrigin(origin, cfg.Server.CORSOrigins) {
                w.Header().Set("Access-Control-Allow-Origin", origin)
                w.Header().Set("Access-Control-Allow-Methods",
                    "GET, POST, DELETE, OPTIONS")
                w.Header().Set("Access-Control-Allow-Headers",
                    "Authorization, CE-Token, Content-Type")
                w.Header().Set("Access-Control-Max-Age", "86400")
            }

            if r.Method == "OPTIONS" {
                w.WriteHeader(http.StatusNoContent)
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}

func isAllowedOrigin(origin string, allowed []string) bool {
    for _, pattern := range allowed {
        // Support wildcards: "http://localhost:*"
        if matchOriginPattern(pattern, origin) {
            return true
        }
    }
    return false
}
```

---

## 14. Engine Additions — QuerySync and QueryWithChannels

The engine needs two new entry points for server use.

```go
// internal/runner/runner.go (additions)

// QuerySync runs a query synchronously and returns the complete result.
// Used by MCP tools and REST API POST /query.
func (e *Engine) QuerySync(ctx context.Context, opts QueryOptions) (*QueryResult, error) {
    ch := e.NewChannels()
    defer e.CloseChannels(ch)

    result := &QueryResult{}
    start := time.Now()

    // Accumulate answer from message channel
    var answer string
    var wg sync.WaitGroup
    wg.Add(1)
    go func() {
        defer wg.Done()
        for msg := range ch.Message {
            answer = msg.Content
        }
    }()

    // Run query
    if err := e.QueryWithChannels(ctx, opts.Query, ch, opts); err != nil {
        return nil, err
    }

    wg.Wait()

    result.Answer     = answer
    result.DurationMS = time.Since(start).Milliseconds()

    return result, nil
}

// QueryWithChannels runs a query using caller-provided channels.
// Used by WebSocket handler to stream to multiple clients.
func (e *Engine) QueryWithChannels(
    ctx context.Context,
    query string,
    ch *core.AppChannels,
    opts QueryOptions,
) error {
    // ... implementation delegates to runner with provided channels
}

type QueryOptions struct {
    Query    string
    MaxLoops int
}

type QueryResult struct {
    RunID      string
    Answer     string
    TokensIn   int
    TokensOut  int
    CostUSD    float64
    LoopsUsed  int
    DurationMS int64
    Partial    bool
}
```

---

## 15. Package Layout Summary

```
internal/server/
  server.go               — Server, Start(), Stop(), shutdown()
  mcp/
    mcp.go                — Server, registerTools(), handleRequest()
    stdio.go              — RunStdio(), handleNotification()
    sse.go                — StartSSE(), sseSession, handleSSEConnect/Message
    tools/
      query.go            — ce_query tool + handler
      index.go            — ce_index tool + handler
      status.go           — ce_status tool + handler
      search.go           — ce_search tool + handler
    protocol/
      types.go            — all JSON-RPC 2.0 + MCP types
      marshal.go          — OKResponse(), ErrorResponse() helpers
  api/
    api.go                — Server, New(), Start(), Shutdown(), corsMiddleware()
    auth.go               — Middleware(), extractToken(), validateToken()
    handlers/
      query.go            — POST /api/v1/query
      projects.go         — GET /api/v1/projects, GET /api/v1/projects/{id}
      substrate.go        — GET /api/v1/substrate/nodes, /edges
      execlog.go          — GET /api/v1/execlog, /execlog/{runId}
      tokens.go           — POST/GET/DELETE /api/v1/tokens
      health.go           — GET /health
    ws/
      ws.go               — WebSocket Handler(), WSEvent type
      stream.go           — channel → WebSocket frame streaming
```

---

## 16. Key Decisions Captured in This Spec

| Decision | Value |
|----------|-------|
| MCP transports | Both stdio and SSE — stdio for IDEs, SSE for remote |
| Stdio command | `ce mcp-stdio` — hidden subcommand, called by IDE config |
| SSE path | `/mcp/sse` (connect) + `/mcp/messages` (POST requests) |
| MCP protocol version | 2024-11-05 |
| MCP tools | ce_query, ce_index, ce_status, ce_search |
| API server | REST + WebSocket on same port as MCP SSE |
| Auth | Bearer token in Authorization header or CE-Token header |
| Local auth bypass | 127.0.0.1 requests without token allowed (local-only mode) |
| WebSocket | Per-connection channel set — each WS client gets own channels |
| CORS | Wildcard port support (http://localhost:*) |
| Server process | PID file for ce server stop |
| QuerySync | Synchronous wrapper for MCP + REST — accumulates from channels |
| Token scopes | read (GET only), read-write, admin |
| Write method check | read-scoped tokens blocked on POST/DELETE |
| Execution log API | Full LLM call log with prompts — CE Studio contract |

---

*Spec 14: MCP + API Server — v1.0 — February 2026*
*Next: Spec 15 — CE Studio*
*Companion: Context Engine PRD v0.5 Section 14 | Decisions Log v1.0 Section 10*
