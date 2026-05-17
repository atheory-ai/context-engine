# Data, Privacy, and Security Model

This document explains what CE stores locally, what it can send to configured LLM providers, how API token scopes work, and what audit/execution logs record.

CE is local-first, but not air-gapped by default. If you configure a remote LLM provider, query context is sent to that provider.

## Local Data Directory

CE stores state under the configured data directory. By default:

```text
~/.ce/
```

You can override it with:

```bash
ce --data-dir /path/to/.ce ...
```

or:

```bash
CE_DATA_DIR=/path/to/.ce ce ...
```

Main files:

```text
meta.db              project registry, project paths, token records
audit.db             sessions, turns, audit entries
execution.db         optional verbatim LLM call trace
graphs/local.db      current project graph
graphs/org.db        org-wide graph
plugins/defaults/    extracted default plugin artifacts
cache/plugins/       plugin compilation metadata
cache/wazero/        wazero compilation cache
server.pid           running server PID
```

SQLite databases use WAL mode, so temporary `*.db-wal` and `*.db-shm` files can appear next to the main database files.

## What Is Stored Locally

### Project config

`ce.yaml` can contain:

- project Git URL
- project description
- architecture notes
- LLM provider and model configuration
- index include/exclude rules
- plugin paths and plugin config
- server settings

Do not commit real API keys in `ce.yaml`. Prefer environment variables such as `ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, or `CE_LLM_API_KEY`.

### Graph data

Project graph databases store extracted code intelligence:

- files
- symbols
- namespaces
- concepts
- imports/calls/references/implements/extends edges
- activation and edge-weight metadata
- concept seeds
- enrichment records

The graph is derived from the indexed codebase. Treat graph databases as potentially sensitive because labels, canonical IDs, file paths, properties, and relationships can reveal source code structure and domain concepts.

### Plugin artifacts and cache

CE can store:

- default plugin `.wasm` files extracted from release binaries
- user plugin paths from `ce.yaml`
- plugin compilation/cache metadata
- wazero cache files

Plugin cache files are local implementation details. Clear them with:

```bash
ce cache clear
```

### Tokens

Token records live in `meta.db`. The schema stores:

- token ID/value
- human-readable name
- scope
- creation time
- optional expiry
- last-used time
- revoked flag
- properties JSON

Token values are bearer credentials. Store and transmit them like passwords.

Note: the current CLI `ce token` commands are still Phase 1-stubbed. Server-side token validation and REST token handlers exist, but contributor docs should not assume the CLI token flow is complete until that implementation is finished.

## What Is Sent To LLM Providers

When using a remote LLM provider, CE sends prompt messages through the configured provider adapter. Depending on the query phase, prompts can include:

- the user's query
- project description and architecture notes from config
- strategizer, reviewer, and synthesizer system prompts
- selected graph context from activation
- file paths, symbol names, namespaces, concept names, and relationships
- tool outputs and channel emissions
- prior loop findings within the same query

CE does not need to send the whole repository for every query. It sends selected context assembled by the runner, agents, activation layer, and tools. That selected context can still contain sensitive code structure or excerpts depending on the tools involved.

Remote provider behavior is governed by that provider's terms and configuration. Use the `local` provider only when implemented and configured for a local model path; the current local provider is still experimental/stubbed.

## What Is Not Intentionally Sent

CE should not intentionally send these to LLM providers:

- API keys
- CE API tokens
- raw SQLite database files
- plugin binaries
- the whole data directory
- unrelated files outside indexed/project context

This is a design expectation, not a substitute for review. Be careful when adding tools or prompt construction paths that include raw config, environment, logs, or broad file contents.

## Execution Tracing

`execution.db` is the sensitive trace database. When tracing is enabled, CE records verbatim LLM call data:

- prompt messages
- response text
- thinking trace, when present
- emitted IR for strategizer calls, when present
- model and tier
- token counts
- estimated cost
- latency/context metrics
- run, turn, and session IDs

Execution tracing is controlled by `TracingConfig` and the `--trace` query flag. Code paths are designed so execution logging is skipped when:

- tracing is disabled
- the session is read-only
- `execution.db` is not open

Treat `execution.db` as highly sensitive. It can contain user queries, selected code context, model responses, and potentially proprietary reasoning traces.

## Audit Log Behavior

`audit.db` is intended to be the append-only operational record:

- sessions
- turns
- actor IDs
- token IDs
- surface (`cli`, `tui`, `mcp`, `api`, `ws`)
- actions
- project IDs
- token scope at the time of action
- status and error messages
- timestamps
- properties JSON

Audit records are separate from verbatim LLM execution traces. The audit log answers "who did what when"; the execution log answers "what exactly was sent to and returned from the LLM."

The preflight path creates sessions and turns for query execution. Audit coverage should expand as server/API actions mature.

## Token Scopes

Built-in token scopes:

| Scope | Intended use |
| ----- | ------------ |
| `read` | read-only API/MCP access |
| `read-write` | read plus write operations such as query/index/token creation, depending on handler |
| `admin` | administrative access for future privileged operations |

Current API middleware enforces:

- missing token returns unauthorized unless the request is local and the server is bound to `127.0.0.1`
- expired tokens are rejected
- revoked tokens are rejected
- `read` tokens cannot perform write HTTP methods: `POST`, `PUT`, `PATCH`, or `DELETE`

At present, `admin` is reserved for future privileged operations. Middleware distinguishes `read` from non-read scopes; endpoint-level admin-only behavior should be documented and tested when added.

Accepted token locations:

- `Authorization: Bearer <token>`
- `CE-Token: <token>`
- `?token=<token>` query parameter, mainly for WebSocket clients

Avoid query-parameter tokens except when the client cannot send headers, because URLs are more likely to appear in logs and browser history.

## Localhost Auth Behavior

When the server is bound to `127.0.0.1`, local requests from loopback can be allowed without a token. This is intended for local development and Studio-on-localhost workflows.

For any remote or shared environment:

- bind intentionally
- use tokens
- prefer HTTPS or a trusted local network tunnel in front of CE
- restrict CORS origins
- do not expose CE directly to the public internet

## Read-Scoped Sessions

Read-scoped token sessions are security-sensitive. They must not write to:

- `execution.db`
- project graph databases
- org graph database

This is an architectural constraint. Code handling read-scoped sessions should preserve `cfg.ReadOnly` and avoid write paths.

## Plugin Security

Plugins are WebAssembly modules loaded by CE through Extism and wazero. They should be treated as code, not data.

Runtime expectations:

- no Node.js runtime APIs inside plugins
- no filesystem, process, network, or native extension dependency inside plugin code
- host functions are the integration boundary
- substrate access from plugin helpers is read-only
- plugin emissions are limited to `thinking`, `action`, `debug`, and `warning`

Only install plugins from sources you trust. Validate plugins before use:

```bash
ce plugin validate /path/to/plugin.wasm
```

## Backups And Sharing

Before sharing logs or database files, assume they may contain sensitive data.

High sensitivity:

- `execution.db`
- `audit.db`
- `meta.db`
- `graphs/*.db`
- `ce.yaml` if it contains API keys or private architecture notes

Lower sensitivity but still local-state-bearing:

- plugin cache directories
- `server.pid`
- generated coverage/test artifacts

For support requests, prefer command output with secrets redacted over raw database uploads.

## Contributor Checklist

When changing data, auth, LLM, server, or plugin behavior:

- identify whether the change affects local storage, LLM payloads, token scopes, or audit/execution logs
- update this document if the data model changes
- do not add config/environment dumps to prompts
- keep token values out of logs and golden files
- preserve read-only session restrictions
- add tests for token scope, trace skipping, or audit behavior when practical
- update [stability.md](./stability.md) if an external contract changes

## Related Docs

- [Architecture guide](./architecture.md)
- [Roadmap and stability](./stability.md)
- [Troubleshooting](./troubleshooting.md)
- [Plugin authoring](./plugin-authoring.md)
- [Spec 1: Data Layer](./specs/1-spec-data-layer.md)
- [Spec 12: LLM](./specs/12-spec-12-llm.md)
- [Spec 14: Server](./specs/14-spec-14-server.md)
