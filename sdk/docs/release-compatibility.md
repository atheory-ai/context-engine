# Release Compatibility

This page keeps the plugin SDK repository aligned with its sibling
repositories:

- `context-engine`: CE runtime, CLI, server, storage, MCP/API, and release binary
- `ce-plugin-sdk`: TypeScript plugin SDK, sandbox, templates, and default plugins
- `atheory-ce-studio`: developer inspector UI for CE

## Versioning

The three repositories use the same SemVer release train. A coordinated release
uses the same `vX.Y.Z` tag in each repository.

| Repository | Version source | Published artifact |
| --- | --- | --- |
| `context-engine` | Git tag `vX.Y.Z` | `ce` binaries and archives |
| `ce-plugin-sdk` | Package versions and Git tag `vX.Y.Z` | `@atheory-ai/ce-plugin-sdk`, `@atheory-ai/ce-plugin-sandbox`, `@atheory-ai/create-ce-plugin`, and default plugin WASM artifacts |
| `atheory-ce-studio` | `package.json` and Git tag `vX.Y.Z` | `@atheory-ai/ce-studio` npm package and GitHub release bundle |

Patch releases may ship from one repository when the change is isolated, but
release notes must still state which sibling versions were tested. Minor and
major releases should be coordinated across all three repositories.

Within this repo, workspace packages that are published together should share
the release version. Default plugins may also carry plugin manifest versions,
but their runtime compatibility is determined by the CE plugin ABI and the SDK
version used to build them.

## Compatibility Matrix

| CE version | SDK version | Studio version | Status | Notes |
| --- | --- | --- | --- | --- |
| `0.1.x` | `0.1.x` | `0.1.x` | Supported development line | Use matching minor versions. CE release CI validates this repo before publishing CE. |

Compatibility means:

- CE can load default plugin artifacts built from this repo.
- SDK-generated plugin manifests and WASM exports match CE runtime expectations.
- Studio can connect to the CE REST, WebSocket, and MCP endpoints it uses.
- Release notes identify any required sibling repo upgrade.

## Release Notes

Each repository keeps its own `CHANGELOG.md`, but release notes should use the
same headings when relevant:

- `Runtime`: CE binary, storage, server, MCP/API, indexing, runner behavior.
- `Plugin SDK`: SDK APIs, sandbox behavior, scaffold templates, default plugins.
- `Studio`: UI, graph/query/trace workflows, API assumptions.
- `Compatibility`: required sibling versions, migration notes, or known gaps.

When SDK changes alter generated plugin manifests, WASM exports, host function
usage, or default plugin behavior, call out the matching CE version under
`Compatibility`.

## Local Development Linking

The sibling repos can be developed side by side:

```text
Sites/
  atheory-ce/
  ce-plugin-sdk/
  atheory-ce-studio/
```

### SDK packages inside this workspace

The workspace uses `workspace:*` dependencies, so SDK packages and default
plugins are already linked to the local `packages/plugin-sdk` source after
install:

```bash
cd ../ce-plugin-sdk
pnpm install
pnpm build
pnpm test
```

### SDK plugins with local CE

Build CE locally:

```bash
cd ../atheory-ce
go build -o ce ./cmd/ce
```

Build default plugins from this repo:

```bash
cd ../ce-plugin-sdk
pnpm build
```

Copy the plugin WASM files into the CE data directory used by your local run:

```bash
mkdir -p ~/.ce/plugins/defaults
cp plugins/go-language/dist/go-language.wasm ~/.ce/plugins/defaults/
cp plugins/typescript-language/dist/typescript.wasm ~/.ce/plugins/defaults/
cp plugins/python-language/dist/python.wasm ~/.ce/plugins/defaults/
```

For an isolated local run:

```bash
mkdir -p /tmp/ce-dev/plugins/defaults
cp plugins/go-language/dist/go-language.wasm /tmp/ce-dev/plugins/defaults/
cp plugins/typescript-language/dist/typescript.wasm /tmp/ce-dev/plugins/defaults/
cp plugins/python-language/dist/python.wasm /tmp/ce-dev/plugins/defaults/

cd ../atheory-ce
CE_DATA_DIR=/tmp/ce-dev ./ce server start
```

### Studio with local CE

Start CE, then run Studio:

```bash
cd ../atheory-ce
CE_DATA_DIR=/tmp/ce-dev ./ce server start

cd ../atheory-ce-studio
pnpm install
pnpm dev
```

Studio defaults to `http://localhost:8765`. Change the URL in Studio settings if
the local CE server uses a different address.

## Release Checklist

Before publishing a coordinated release:

1. Update versions and changelogs in all affected repos.
2. Confirm this compatibility matrix still matches the intended release train.
3. Run each repo's documented verification commands.
4. For CE releases, confirm release CI passes SDK and Studio compatibility jobs.
5. Include sibling repo requirements in release notes.
