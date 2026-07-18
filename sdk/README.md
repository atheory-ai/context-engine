# CE Plugin SDK

The plugin SDK for [Context Engine](https://github.com/atheory-ai/context-engine). Plugins are TypeScript modules that compile to WebAssembly — they teach CE how to index a language or framework, what concepts it contributes, and optionally add custom analysis passes and tools.

---

## Repository role

This `sdk/` workspace owns the TypeScript plugin ecosystem for CE:
`@atheory-ai/ce-plugin-sdk`, `@atheory-ai/ce-plugin-sandbox`,
`@atheory-ai/create-ce-plugin`, default language plugin source, examples, and
plugin-focused documentation. It lives inside the
[Context Engine repository](https://github.com/atheory-ai/context-engine).

---

## Monorepo structure

```text
packages/
  plugin-sdk/          — @atheory-ai/ce-plugin-sdk: types, definePlugin(), host bindings, ESLint rules
  plugin-sandbox/      — @atheory-ai/ce-plugin-sandbox: build + test + validate plugins locally
  create-ce-plugin/    — @atheory-ai/create-ce-plugin: scaffold a new plugin with `pnpm create @atheory-ai/ce-plugin`

plugins/
  go-language/         — default Go plugin (functions, types, interfaces, imports)
  typescript-language/ — default TypeScript/JS/JSX/TSX plugin
  python-language/     — default Python plugin

examples/
  hello-world/         — minimal language plugin
  go-language/         — annotated reference implementation

llm-skills/            — markdown prompts for AI-assisted plugin generation

scripts/               — release and package-maintenance scripts
```

---

## Prerequisites

| Tool | Version | Notes |
| ---- | ------- | ----- |
| Node.js | 20+ | |
| pnpm | 11.1.3+ | Enable through Corepack |
| `ce` binary | Latest | Required for `ce plugin validate` and sandbox testing |

`@atheory-ai/wasm-plugin-toolkit` provides the supported JavaScript-to-WASM
build path for plugins and manages the Javy compiler it needs.

---

## Getting started

```bash
git clone https://github.com/atheory-ai/context-engine.git
cd context-engine/sdk

# Installs workspace dependencies
pnpm install
```

### Build the default plugins

```bash
# Build all three default plugins
pnpm build

# Or build individually
pnpm --filter go-language-plugin build
pnpm --filter typescript-language-plugin build
pnpm --filter python-language-plugin build
```

Built `.wasm` files land in each plugin's `dist/` directory:

- `plugins/go-language/dist/go-language.wasm`
- `plugins/typescript-language/dist/typescript.wasm`
- `plugins/python-language/dist/python.wasm`

To use them with a local CE build, copy to the defaults directory:

```bash
mkdir -p ~/.ce/plugins/defaults
cp plugins/go-language/dist/go-language.wasm ~/.ce/plugins/defaults/
cp plugins/typescript-language/dist/typescript.wasm ~/.ce/plugins/defaults/
cp plugins/python-language/dist/python.wasm ~/.ce/plugins/defaults/
```

### Run tests

```bash
pnpm test                               # all plugins
pnpm --filter go-language-plugin test
```

---

## Creating a new plugin

```bash
pnpm create @atheory-ai/ce-plugin
```

Prompts for name, language, and extensions, then scaffolds the full project structure with `package.json`, `tsconfig.json`, `ce-plugin.json`, and a starter `src/index.ts`.

---

## Plugin API

Plugins call `definePlugin()` from `@atheory-ai/ce-plugin-sdk`. The compiled WASM exports well-known functions that CE calls at index time and query time.

### Minimal language plugin

```typescript
import { definePlugin } from '@atheory-ai/ce-plugin-sdk';

definePlugin({
  id: 'my-org.my-language',
  name: 'My Language',
  version: '1.0.0',
  description: 'Indexes My Language files',

  language: {
    extensions: ['.ml'],

    extract(filePath, content, treeJSON) {
      const nodes = [];
      const edges = [];

      nodes.push({
        type: 'symbol',
        label: 'MyFunction',
        canonicalId: `${filePath}:MyFunction`,
        properties: { exported: true, kind: 'function' },
      });

      return { nodes, edges };
    },

    concepts: [
      { term: 'pattern-matching', definition: 'Structural decomposition of values' },
    ],
  },
});
```

`extract` receives `treeJSON` — the host's tree-sitter CST. For a language the
engine doesn't bundle (go, python, javascript, typescript, tsx), ship a
tree-sitter grammar WASM and add `grammar: 'my-language.wasm'` to `language`;
the host loads it at runtime. A plugin can also lift each function to
**intent** — return `iir` alongside `nodes`/`edges` — and contribute conformance
rules via `iirRules`. See the runtime's `docs/iir.md`.

### Node types

| Type | When to use |
| ---- | ----------- |
| `symbol` | Functions, methods, classes, variables |
| `namespace` | Packages, modules, directories |
| `concept` | Domain vocabulary (not a code entity) |
| `file` | Source files (created automatically — plugins don't need to emit these) |

Key `symbol` properties: `exported: boolean`, `kind: 'function' | 'class' | 'interface' | 'variable' | ...`

### Edge types

| Type | Meaning |
| ---- | ------- |
| `imports` | File/namespace imports another namespace |
| `calls` | Symbol calls another symbol |
| `implements` | Class/type implements an interface |
| `extends` | Class/type extends another |
| `defines` | Namespace/file defines a symbol |
| `concept_of` | Symbol is an instance of a concept |
| `depends_on` | Generic dependency |

### Analyzers

Analyzers run after extraction and can add additional edges based on the full set of nodes extracted from a file:

```typescript
definePlugin({
  // ...
  analyzers: [{
    name: 'interface-impl',
    description: 'Detect interface implementations',
    analyze(nodes) {
      const edges = [];
      // Receive all nodes from this file, return additional edges
      return edges;
    },
  }],
});
```

### Concepts (vocabulary seeds)

Concepts are domain vocabulary that the engine uses to understand intent in queries. Contribute them per-language:

```typescript
concepts: [
  {
    term: 'dependency-injection',
    definition: 'Passing dependencies as arguments rather than constructing them',
    related: ['inversion-of-control', 'service-locator'],
    synonyms: ['DI', 'IoC'],
  },
]
```

---

## Build pipeline

Each plugin's build goes through two steps:

```text
TypeScript → esbuild (bundle to ESM) → bin/javy (compile to WASM)
```

The `javy` binary is downloaded automatically on `pnpm install` via `scripts/install-javy.js`. It is placed at `bin/javy` (gitignored). Plugin build scripts reference it as `../../bin/javy`.

To update the javy version, change `JAVY_VERSION` in `scripts/install-javy.js`, delete `bin/javy`, then re-run `pnpm install`.

---

## Validating a plugin

```bash
# Requires ce binary in PATH
pnpm --filter go-language-plugin validate

# Or directly
ce plugin validate plugins/go-language/dist/go-language.wasm
```

Validation checks:

- Required WASM exports are present (`ce_plugin_manifest`, `ce_language_match`, `ce_language_extract`, etc.)
- Manifest fields are valid (semver version, `org.name` ID format, description ≤ 100 chars)
- Plugin loads without panicking

---

## Sandbox testing

The `@atheory-ai/ce-plugin-sandbox` package provides a CLI for testing plugins against fixture files before deploying:

```bash
# From a plugin directory
pnpm sandbox run fixtures/simple-service.go
# Shows: extracted nodes, edges, coverage %, concept seeds
```

Fixture files live in `plugins/<name>/fixtures/` and are plain source files in the target language.

The sandbox report includes:

- All extracted nodes with types and canonical IDs
- All extracted edges with types and weights
- Coverage percentage (symbols extracted / symbols estimated)
- Concept seeds the plugin contributes

---

## Adding a plugin to CE

Add the built WASM path to `ce.yaml` in your project:

```yaml
plugins:
  - path: /path/to/my-plugin/dist/my-plugin.wasm
    config:
      # optional config, available to the plugin via __ce_get_config()
      some_option: value
```

User plugins take precedence over default plugins. If your plugin handles the same extensions as a default plugin (e.g. `.ts`), yours wins for those files.

## Project docs

- [Release compatibility](./docs/release-compatibility.md) — aligned versioning, compatibility matrix, release notes, and local dev linking
- [LICENSE](./LICENSE) — license terms
- [CONTRIBUTING.md](./CONTRIBUTING.md) — contributor workflow and verification steps
- [CODE_OF_CONDUCT.md](./CODE_OF_CONDUCT.md) — community expectations
- [SECURITY.md](./SECURITY.md) — private vulnerability reporting
- [CHANGELOG.md](./CHANGELOG.md) — notable project changes
- [RELEASING.md](./RELEASING.md) — maintainer release process

---

## Package details

### @atheory-ai/ce-plugin-sdk

The core SDK. Build it before working on plugins:

```bash
pnpm --filter @atheory-ai/ce-plugin-sdk build
```

Provides:

- `definePlugin(config)` — validates and registers your plugin at definition time
- TypeScript types (`Node`, `Edge`, `ConceptSeed`, `ExtractionResult`, etc.)
- Host function declarations (`__ce_emit`, `__ce_substrate_query`, `__ce_get_config`, etc.) provided by CE at runtime via wazero
- ESLint plugin with plugin-authoring rules (no Node.js APIs, no network, etc.)

### @atheory-ai/ce-plugin-sandbox

Build + run + validate loop for local development:

```bash
pnpm --filter @atheory-ai/ce-plugin-sandbox build
npx ce-sandbox run <fixture> --plugin <path-to.wasm>
```

Shells out to the `ce` binary for the actual WASM loading (same runtime as production).

### @atheory-ai/create-ce-plugin

Interactive scaffolding CLI:

```bash
pnpm create @atheory-ai/ce-plugin
```

---

## Plugin manifest (ce-plugin.json)

Each plugin directory contains a `ce-plugin.json` describing the plugin:

```json
{
  "id": "my-org.my-language",
  "name": "My Language",
  "version": "1.0.0",
  "description": "Indexes My Language files",
  "extensions": [".ml"],
  "capabilities": ["language", "analyzer"]
}
```

This file is read by the sandbox and by `ce plugin list`.

---

## LLM skills

The `llm-skills/` directory contains 8 markdown files designed to be passed as context to an LLM for autonomous plugin generation. They cover the full plugin authoring workflow — from understanding the spec to implementing extraction logic for a new language.

---

## Related repos

- [context-engine](https://github.com/atheory-ai/context-engine) — the engine that loads and runs these plugins
- [atheory-ce-studio](https://github.com/atheory-ai/atheory-ce-studio) — developer inspector UI
