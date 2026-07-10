# @atheory-ai/ce

Context Engine (`ce`) — an AI-powered coding assistant that builds a persistent
knowledge graph of your codebase and reasons over it. Index your code once
(symbols, namespaces, dependencies, concepts), then answer questions with
precise, grounded context.

This package is a thin wrapper that installs the matching platform binary and
runs the native `ce` executable. The engine is pure Go (no native build step).

## Install

```bash
npm install -g @atheory-ai/ce
ce version
```

## Quick start

```bash
export ANTHROPIC_API_KEY=sk-ant-...
cd your-project
ce project init      # register the project, create ce.yaml
ce index .           # build the knowledge graph
ce server start      # expose it over MCP + REST for your agent/IDE
```

`ce server` serves an MCP endpoint (`/mcp/sse`, plus a `ce mcp-stdio` transport
for Claude Desktop / Cursor / Claude Code) and a REST API. It also offers
deterministic tools — search, references, call graph, file context, cross-project
matches, and more — for agent harnesses to drive.

## Documentation

Full docs, commands, and configuration:
**https://github.com/atheory-ai/context-engine#readme**

- Architecture: `docs/architecture.md`
- Intent representation (IIR): `docs/iir.md`
- Plugin authoring: `docs/plugin-authoring.md`

## License

See the [repository](https://github.com/atheory-ai/context-engine).
