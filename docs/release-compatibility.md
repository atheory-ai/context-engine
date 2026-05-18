# Release Compatibility

This page keeps the Context Engine repository aligned with its sibling
repositories:

- `context-engine`: CE runtime, CLI, server, storage, MCP/API, and release binary
- `ce-plugin-sdk`: TypeScript plugin SDK, sandbox, templates, and default plugins
- `atheory-ce-studio`: developer inspector UI for CE

## Versioning

The three repositories use the same SemVer release train. A coordinated release
uses the same `vX.Y.Z` tag in each repository.

| Repository | Version source | Published artifact |
| --- | --- | --- |
| `context-engine` | `VERSION` file and Git tag `vX.Y.Z` | `ce` binaries, archives, GitHub installer bundle, and `@atheory-ai/ce` npm packages |
| `ce-plugin-sdk` | Package versions and Git tag `vX.Y.Z` | SDK packages, sandbox, scaffolder, default plugin WASM artifacts |
| `atheory-ce-studio` | `package.json` and Git tag `vX.Y.Z` | `@atheory-ai/ce-studio` npm package and GitHub release bundle |

Patch releases may ship from one repository when the change is isolated, but
release notes must still state which sibling versions were tested. Minor and
major releases should be coordinated across all three repositories.

## Compatibility Matrix

| CE version | SDK version | Studio version | Status | Notes |
| --- | --- | --- | --- | --- |
| `0.1.x` | `0.1.x` | `0.1.x` | Supported development line | Use matching minor versions. CE release CI validates the sibling repos before publishing. |

Compatibility means:

- CE can load default plugin artifacts built from the SDK repo.
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

When a CE release depends on SDK or Studio changes, call that out under
`Compatibility` before tagging.

## Local Development Linking

The sibling repos can be developed side by side:

```text
Sites/
  atheory-ce/
  ce-plugin-sdk/
  atheory-ce-studio/
```

### CE with local SDK plugins

Build CE locally:

```bash
cd ../atheory-ce
go build -o ce ./cmd/ce
```

Build default plugins from the local SDK repo:

```bash
cd ../ce-plugin-sdk
pnpm install
pnpm build
```

Copy the plugin WASM files into the CE data directory used by your local run:

```bash
mkdir -p ~/.ce/plugins/defaults
cp plugins/go-language/dist/go-language.wasm ~/.ce/plugins/defaults/
cp plugins/typescript-language/dist/typescript.wasm ~/.ce/plugins/defaults/
cp plugins/python-language/dist/python.wasm ~/.ce/plugins/defaults/
```

For an isolated local run, point CE at a throwaway data directory:

```bash
cd ../atheory-ce
CE_DATA_DIR=/tmp/ce-dev ./ce server start
```

If you use `CE_DATA_DIR=/tmp/ce-dev`, copy the plugin WASM files into
`/tmp/ce-dev/plugins/defaults/` instead of `~/.ce/plugins/defaults/`.

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
