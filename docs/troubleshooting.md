# Troubleshooting

This guide covers the issues contributors and early users are most likely to hit when running CE locally: plugin loading, SQLite state, MCP setup, and Studio connections.

## First Checks

Start with these commands:

```bash
ce --help
ce config show
ce project status
ce plugin list
ce server status
```

If you are using a non-default data directory, pass it every time:

```bash
ce --data-dir /path/to/.ce config show
ce --data-dir /path/to/.ce server status
```

Enable debug output when a command fails without enough detail:

```bash
ce --debug index .
ce --debug query "what handles authentication?"
```

## Plugin Loading

### `ce plugin list` shows no language plugins

Local source builds do not automatically include production plugin artifacts unless they were embedded at build time. Production release binaries embed default language and convention plugins and extract them on first run.

For local development:

1. Build default plugins from [ce-plugin-sdk](https://github.com/atheory-ai/ce-plugin-sdk).
2. Place the `.wasm` files under the CE data directory:

```text
~/.ce/plugins/defaults/
```

3. Re-run:

```bash
ce plugin list
```

If you use `--data-dir`, place the plugins under that directory instead:

```text
/path/to/.ce/plugins/defaults/
```

### A plugin fails validation

Run validation directly against the `.wasm` file:

```bash
ce plugin validate /path/to/plugin.wasm
```

Common causes:

- the file is not a WebAssembly module
- the module does not export `ce_plugin_manifest`
- the manifest JSON is invalid
- the plugin was built for a different runtime contract
- a relative grammar path in the manifest does not resolve next to the plugin file

### A plugin was rebuilt but CE behaves like the old version

Clear the plugin compilation cache:

```bash
ce cache clear
```

If you are debugging plugin cache behavior, also inspect:

```text
~/.ce/cache/plugins/
~/.ce/cache/wazero/
```

Do not edit cache files by hand as a normal fix. Clear the cache and let CE rebuild it.

### Files are skipped during indexing

Check:

- `indexer.include` and `indexer.exclude` in `ce.yaml`
- whether a loaded language plugin handles the file extension
- whether `ce plugin list` shows the expected plugin
- whether the file lives under ignored directories such as `vendor/` or `node_modules/`

Run a full reindex after changing plugin or include/exclude settings:

```bash
ce index . --full
```

## SQLite Files

CE stores local state in SQLite under the configured data directory. By default this is:

```text
~/.ce/
```

The main files are:

```text
meta.db              project registry, config metadata, API tokens
audit.db             sessions, turns, access log
execution.db         run and tool execution records
graphs/local.db      current project graph
graphs/org.db        org-wide graph
server.pid           running server PID, when the server is active
```

### `CE data directory not found`

Initialize a project first:

```bash
ce project init .
```

Or point CE at the data directory you intended to use:

```bash
ce --data-dir /path/to/.ce project status
```

### Project status or queries see the wrong graph

Check the active project and data directory:

```bash
ce config show
ce project list
ce project status
```

If you use multiple CE data directories, make sure the same `--data-dir` value is used for indexing, querying, server startup, and Studio connection.

### SQLite lock or stale server issues

First check the server state:

```bash
ce server status
ce server stop
```

Then retry the command. If `server.pid` points to a process that no longer exists, remove only the stale PID file from the relevant data directory:

```text
~/.ce/server.pid
```

Avoid deleting `.db` files unless you intentionally want to reset local CE state. To rebuild the project graph without deleting all metadata, prefer:

```bash
ce index . --full
```

### Inspecting SQLite files

Use the SQLite CLI only for inspection:

```bash
sqlite3 ~/.ce/meta.db '.tables'
sqlite3 ~/.ce/graphs/local.db '.tables'
```

Do not manually write to graph databases. CE substrate writes must go through the write buffer.

## MCP Setup

CE supports MCP over stdio for local IDE integrations and SSE for HTTP clients.

### Local stdio MCP

Use this shape in the MCP client config:

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

If the client cannot start CE:

- confirm `ce` is on the client's `PATH`
- use an absolute path to the binary if the client has a restricted environment
- pass `--data-dir` in `args` if you do not use the default data directory
- include provider environment variables such as `ANTHROPIC_API_KEY` if the client does not inherit your shell environment

Example with explicit binary and data directory:

```json
{
  "mcpServers": {
    "ce": {
      "command": "/usr/local/bin/ce",
      "args": ["--data-dir", "/Users/me/.ce", "mcp-stdio"],
      "env": {
        "ANTHROPIC_API_KEY": "..."
      }
    }
  }
}
```

### HTTP MCP SSE

Start the server:

```bash
ce server start
```

The SSE endpoint is:

```text
http://127.0.0.1:4040/mcp/sse
```

If the server does not start:

- check whether another process is using the configured port
- check `ce server status`
- verify `server.host`, `server.port`, and `server.mcp_enabled` in `ce config show`

For remote or non-localhost access, create and pass an API token. Local requests to `127.0.0.1` can be allowed without a token by the server auth middleware, but remote clients should not rely on that behavior.

## Studio Connection

CE Studio is a separate web UI. It connects to a running CE server over REST and WebSocket.

### Start CE server first

```bash
ce server start
ce server status
```

Default local endpoints:

```text
REST:      http://127.0.0.1:4040/api/v1
WebSocket: ws://127.0.0.1:4040/api/v1/ws
Health:    http://127.0.0.1:4040/health
```

If Studio shows disconnected:

- confirm CE server is running
- confirm Studio is pointed at the same host and port CE prints
- use `127.0.0.1` consistently instead of mixing `localhost` and `127.0.0.1`
- check browser devtools for failed REST or WebSocket requests
- check whether a token is required for the host you configured

### REST works but streaming does not

Check the WebSocket endpoint:

```text
ws://127.0.0.1:4040/api/v1/ws
```

Common causes:

- Studio is configured with an old WebSocket path
- a proxy does not support WebSocket upgrade
- CE is listening on a different port than Studio expects
- auth succeeds for REST headers but the WebSocket client is not passing a token

The server accepts tokens from:

- `Authorization: Bearer <token>`
- `CE-Token: <token>`
- `?token=<token>` query parameter, which is useful for WebSocket clients

### Studio sees no project data

Make sure the server is using the same CE data directory that you indexed:

```bash
ce --data-dir /path/to/.ce index . --full
ce --data-dir /path/to/.ce server start
```

Then reconnect Studio to that server.

## Reset Options

Use the narrowest reset that matches the problem:

| Problem | Reset |
| ------- | ----- |
| Plugin cache seems stale | `ce cache clear` |
| Project graph is stale | `ce index . --full` |
| Server PID is stale | stop server, then remove `server.pid` only if the process is gone |
| Wrong data directory | pass the same `--data-dir` to all commands |
| Full local reset | move the data directory aside instead of deleting it immediately |

For a full reset:

```bash
mv ~/.ce ~/.ce.backup
ce project init .
```
