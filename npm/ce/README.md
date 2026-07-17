# @atheory-ai/ce

Context Engine (`ce`) — an AI-powered coding assistant that builds a persistent
knowledge graph of your codebase and reasons over it. Index your code once
(symbols, namespaces, dependencies, concepts), then answer questions with
precise, grounded context.

This package is a thin wrapper around the signed Context Engine GitHub Release
archives. During installation it downloads the archive matching the package
version and your platform, verifies its SHA-256 entry in `checksums.txt`, then
caches and runs the native `ce` executable. It does not contain or install a
separately built platform binary package.

## Install

```bash
npm install -g @atheory-ai/ce
ce version
```

The package version is pinned to the matching release tag. Set
`CE_RELEASE_BASE_URL` only for a trusted GitHub Enterprise or test mirror.

To install without downloading the binary, set `CE_SKIP_DOWNLOAD=1`. The `ce`
command will then explain that it must be reinstalled without `--ignore-scripts`
before it can run.

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
