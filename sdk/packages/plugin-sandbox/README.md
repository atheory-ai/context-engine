# @atheory-ai/ce-plugin-sandbox

Local validation and fixture analysis for Context Engine plugin WASM artifacts.
The sandbox delegates loading and extraction to the `ce` binary, exercising the
same runtime contract used by Context Engine.

## Install

```bash
pnpm add -D @atheory-ai/ce-plugin-sandbox
```

## Commands

```bash
ce-sandbox validate dist/my-plugin.wasm
ce-sandbox run dist/my-plugin.wasm tests/fixtures/example.php
ce-sandbox coverage dist/my-plugin.wasm --fixtures tests/fixtures
ce-sandbox diff dist/previous.wasm dist/current.wasm --fixtures tests/fixtures
```

Use `--ce /path/to/ce` to select a binary and `--json` for machine-readable
output.

## License

Apache-2.0
