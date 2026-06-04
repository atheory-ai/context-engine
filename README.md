# Context Engine

Context Engine (`ce`) is an AI-powered coding assistant that builds a persistent knowledge graph of your codebase and reasons over it. Instead of re-reading files on every query, it indexes your code once — extracting symbols, namespaces, dependencies, and concepts — then uses that graph to answer questions with precise, grounded context.

Upstream repository: [atheory-ai/context-engine](https://github.com/atheory-ai/context-engine). Author and maintainer: Jeremy Harper ([@ladyhunterbear](https://github.com/ladyhunterbear)).

---

## Repository role

This repository owns the CE runtime: the `ce` binary, CLI, runner, storage layer, indexer, MCP server, REST/WebSocket API, and release artifacts. It does not own the TypeScript plugin authoring SDK or the Studio frontend; those live in sibling repositories and are checked during CE releases for compatibility.

Sibling repositories:

- [ce-plugin-sdk](https://github.com/atheory-ai/ce-plugin-sdk) — TypeScript plugin SDK, plugin sandbox, templates, and default plugin source
- [atheory-ce-studio](https://github.com/atheory-ai/atheory-ce-studio) — developer inspector UI for querying, graph exploration, history, and trace inspection

---

## How it works

1. **Index** — The engine walks your project, routes each file to a language plugin, and extracts a property graph (nodes = symbols/namespaces/concepts, edges = calls/imports/implements/etc.) into a local SQLite substrate.
2. **Query** — When you ask a question, a strategizer agent identifies the relevant anchors in the graph. An activation layer propagates through the graph to surface related nodes. Six cognitive tools retrieve call graphs, references, cross-project matches, concepts, file context, and namespace summaries. A reviewer validates and enriches the results. A synthesizer produces the final answer.
3. **Learn** — Hebbian learning strengthens edges between nodes that co-activate during queries. The graph improves the more you use it.

---

## Architecture

```text
~/.ce/
  meta.db        — project registry, paths, settings, API tokens
  audit.db       — sessions, turns, access log
  graphs/
    local.db     — current project's knowledge graph
    org.db       — org-wide graph (cross-project intelligence)

ce.yaml          — project-level config (in your repo root)
```

The cognitive loop:

```text
query → Strategizer → Activation → Fan-out (6 tools) → Reviewer → Synthesizer → answer
```

---

## Prerequisites

| Tool | Version | Notes |
| ---- | ------- | ----- |
| Go | 1.24.3+ | `brew install go` |
| C compiler | Any | For tree-sitter CGO — already present on macOS (Xcode CLT) |
| Language plugins | `.wasm` files | See [ce-plugin-sdk](https://github.com/atheory-ai/ce-plugin-sdk) |

---

## Installation

### npm

For Node-based development environments, install the published wrapper package:

```bash
npm install -g @atheory-ai/ce
ce version
```

The wrapper installs the matching platform package and runs the native `ce`
binary.

### GitHub Releases

For direct binary installation from GitHub Releases:

```bash
curl -fsSL https://raw.githubusercontent.com/atheory-ai/context-engine/main/install.sh | sh
```

Set `CE_INSTALL_DIR` to choose a different install directory:

```bash
curl -fsSL https://raw.githubusercontent.com/atheory-ai/context-engine/main/install.sh | CE_INSTALL_DIR=/usr/local/bin sh
```

The npm package and install script both use the same release binaries produced
by this repository.

### Build from source

```bash
git clone https://github.com/atheory-ai/context-engine.git
cd context-engine
go build -o ce ./cmd/ce

# Or install to $GOPATH/bin:
go install ./cmd/ce
```

### Install default plugins

Default plugins (Go, TypeScript, Python, PHP, WordPress conventions, and WooCommerce conventions) must be built from the plugin SDK and placed in `~/.ce/plugins/defaults/`. See [ce-plugin-sdk](https://github.com/atheory-ai/ce-plugin-sdk) for instructions.

In production releases, plugins are embedded into the binary automatically.

---

## Quick start

```bash
# 1. Set your API key
export ANTHROPIC_API_KEY=sk-ant-...

# 2. Initialize CE in your project
cd ~/your-project
ce project init

# 3. Index the codebase
ce index .

# 4. Ask a question
ce query "how does the payment flow work?"
```

---

## Commands

### `ce project`

```bash
ce project init [path]     # Register a project, create ce.yaml
ce project list            # List all registered projects
ce project status          # Show index state, node/edge counts
```

`ce project init` is interactive — it asks for a project description, architectural notes, and LLM provider. This context is injected into every query.

### `ce index`

```bash
ce index [path]            # Index (or reindex) a project
ce index . --full          # Force complete reindex
ce index . --watch         # Reindex on file changes
ce index . --exclude "vendor/**,*.pb.go"
```

After indexing, nodes are automatically lifted into the org graph (`org.db`) for cross-project intelligence.

### `ce query`

CE v1 is centered on deterministic MCP tools for agent harnesses:

- `ce_status`
- `ce_search`
- `ce_file_context`
- `ce_references`
- `ce_callgraph`
- `ce_summary`
- `ce_concepts`
- `ce_crossproject`
- `ce_source_ranges`
- `ce_investigate`
- `ce_related_tests`
- `ce_entrypoints`
- `ce_lifecycle`

These tools operate on the indexed graph/source context and let the external
agent harness drive the investigation. The old `ce query` / MCP `ce_query`
LLM investigation loop is experimental and disabled by default. For local
development only, enable it with `features.ce_query: true` or
`CE_FEATURES_CE_QUERY=true`.

### `ce server`

```bash
ce server start            # Start MCP + REST API server
ce server start --port 8080
ce server stop             # Stop the running server
ce server status           # Show server address and PID
```

Server exposes:

- `http://localhost:4040/mcp/sse` — MCP SSE endpoint (for IDE integrations)
- `http://localhost:4040/api/v1` — REST API
- `ws://localhost:4040/api/v1/ws` — WebSocket streaming (used by CE Studio)

For IDE integration (Claude Desktop, Cursor, Claude Code), use the hidden stdio transport:

```json
{
  "mcpServers": {
    "ce": {
      "command": "ce",
      "args": ["mcp-stdio"]
    }
  }
}
```

### `ce config`

```bash
ce config show             # Print merged config (all sources)
ce config get llm.provider
ce config set llm.provider anthropic

# Org-level concept vocabulary (shared across all projects)
ce config org-concepts list
ce config org-concepts add --term "event-sourcing" --definition "..."  \
  --related "cqrs,domain-events" --synonyms "event-driven"
ce config org-concepts remove --term "event-sourcing"
```

### `ce plugin`

```bash
ce plugin list             # List loaded plugins and their capabilities
ce plugin validate my.wasm # Validate a plugin's exports and manifest
```

### `ce token`

```bash
ce token create --name "ci" --scope read --expires-days 90
ce token list
ce token revoke <token-id>
```

Tokens are used to authenticate requests to the REST API and MCP server.

### `ce cache`

```bash
ce cache clear             # Clear wazero plugin compilation cache
```

---

## Configuration

CE merges config from three sources (highest to lowest priority):

1. CLI flags
2. `./ce.yaml` (project-level, in your repo)
3. `~/.ce/config.yaml` (global)

**Full `ce.yaml` reference:**

```yaml
project:
  git_url: https://github.com/your-org/your-repo.git
  base_prompt: |
    A Go microservice handling volunteer scheduling and billing.
  arch_prompt: |
    Key packages: api/, scheduler/, billing/.
    Uses event sourcing for billing state.

llm:
  provider: anthropic           # anthropic | openai | local
  api_key: ""                   # or set ANTHROPIC_API_KEY env var
  base_url: ""                  # override for proxies
  models:
    fast: claude-haiku-4-5-20251001
    standard: claude-sonnet-4-6
    thinking: claude-opus-4-6

engine:
  max_loops: 5                  # max cognitive loop iterations
  k_limit: 20                   # top-K activated nodes per turn

indexer:
  include: []                   # glob patterns to include
  exclude:                      # glob patterns to exclude
    - "vendor/**"
    - "node_modules/**"
    - "*.pb.go"
  workers: 8                    # parallel extraction workers

server:
  host: "127.0.0.1"
  port: 4040
  mcp_enabled: true

data:
  dir: "~/.ce"                  # override data directory

plugins:
  - path: /path/to/my-plugin.wasm
    config:
      some_option: value
```

**Environment variables** (all prefixed `CE_`):

| Variable | Purpose |
| -------- | ------- |
| `ANTHROPIC_API_KEY` | Anthropic API key |
| `CE_LLM_API_KEY` | Override any LLM API key |
| `CE_DATA_DIR` | Override data directory |
| `CE_DEBUG` | Enable debug output |

---

## Development

```bash
# Build the CLI
make build

# Run the standard verification suite
make verify

# Run all Go tests
make test

# Run tests for a specific package
go test ./internal/graph/activation/...
go test ./internal/tools/...

# Run tests with the race detector
make test-race

# Format Go files
make fmt
```

### Key packages

| Package | Purpose |
| ------- | ------- |
| `internal/core/` | Types, interfaces, constants — imports nothing internal |
| `internal/runner/` | Engine, cognitive loop wiring |
| `internal/agent/` | Strategizer, reviewer, synthesizer |
| `internal/graph/` | Substrate reader/writer, activation propagation, Hebbian learning |
| `internal/tools/` | Six cognitive tools |
| `internal/indexer/` | File walker, plugin dispatch, incremental hashing |
| `internal/orggraph/` | Org-level graph, cross-project edge detection |
| `internal/llm/` | LLM router, Anthropic provider, retry logic |
| `internal/plugins/` | wazero + Extism plugin runtime |
| `internal/server/` | MCP stdio/SSE, REST API, WebSocket |
| `internal/storage/` | SQLite databases, migrations, write buffer, queries |
| `tui/` | Bubbletea TUI |

### Adding a new cognitive tool

1. Create `internal/tools/mytool/mytool.go` implementing `core.Tool`
2. Add it to the DAG in `internal/runner/toollist.go`
3. Implement `Activate(ir IR) bool` (when should this tool run?)
4. Implement `Execute(ctx, req ToolRequest) (ToolResult, error)`

### Architectural constraints

- `internal/core` imports nothing internal — it is the dependency floor
- All substrate writes go through the write buffer — never write directly to graph DBs
- No CGO except for tree-sitter (use `modernc.org/sqlite` everywhere else)
- Agents take `*core.AgentContext` — never import `runner`

---

## Related repos

- [ce-plugin-sdk](https://github.com/atheory-ai/ce-plugin-sdk) — plugin development kit, default language plugins
- [atheory-ce-studio](https://github.com/atheory-ai/atheory-ce-studio) — developer inspector UI

## Project docs

- [Architecture guide](./docs/architecture.md) — contributor-facing codebase map
- [Golden tests](./docs/golden-tests.md) — how to add and review golden fixtures
- [Plugin authoring](./docs/plugin-authoring.md) — CE runtime expectations linked to SDK APIs
- [Release compatibility](./docs/release-compatibility.md) — aligned versioning, compatibility matrix, release notes, and local dev linking
- [Roadmap and stability](./docs/stability.md) — stable vs experimental API surfaces
- [Data, privacy, and security model](./docs/security-model.md) — local storage, LLM payloads, tokens, and audit logs
- [Troubleshooting](./docs/troubleshooting.md) — plugin loading, SQLite files, MCP, and Studio connection issues
- [LICENSE](./LICENSE) — license terms
- [CONTRIBUTING.md](./CONTRIBUTING.md) — contributor workflow and verification steps
- [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md) — community expectations
- [SECURITY.md](./SECURITY.md) — private vulnerability reporting
- [CHANGELOG.md](./CHANGELOG.md) — notable project changes
- [RELEASING.md](./RELEASING.md) — maintainer release process

---

## Release builds

Releases use GoReleaser with CGO enabled for all platforms:

```bash
make release-snapshot
```

Binaries land in `dist/`. The release workflow validates builds for darwin, linux, and windows on amd64 and arm64 with Zig-backed CGO cross-compilation. The release pipeline embeds compiled WASM plugins from `ce-plugin-sdk` into the binary.

Release publishing also creates npm packages:

- `@atheory-ai/ce`
- `@atheory-ai/ce-darwin-arm64`
- `@atheory-ai/ce-darwin-x64`
- `@atheory-ai/ce-linux-arm64`
- `@atheory-ai/ce-linux-x64`
- `@atheory-ai/ce-win32-arm64`
- `@atheory-ai/ce-win32-x64`
