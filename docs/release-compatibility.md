# Release Compatibility

Context Engine releases a runtime binary and maintains an in-tree TypeScript
SDK workspace. CE Studio is a separate client repository. They are versioned
independently; compatibility is an explicit tested claim, not an assumption
based on matching version numbers.

## Distribution status

| Component | Version source | Current distribution |
| --- | --- | --- |
| CE runtime | Root `VERSION` and Git tag `vX.Y.Z` | Signed GitHub Release archives and the curl installer |
| Plugin SDK | Package version and `plugin-sdk-vX.Y.Z` tag | npm package `@atheory-ai/ce-plugin-sdk` |
| Plugin Sandbox | Package version and `plugin-sandbox-vX.Y.Z` tag | npm package `@atheory-ai/ce-plugin-sandbox` |
| Create CE Plugin | Package version and `create-ce-plugin-vX.Y.Z` tag | npm package `@atheory-ai/create-ce-plugin` |
| CE Studio | Studio package version and tag | Studio's own release process |

Homebrew is planned. The `@atheory-ai/ce` npm wrapper is implemented but not
yet published. Both consume the signed GitHub Release binary for the exact CE
version; neither is part of the runtime tag workflow yet.

## Runtime and SDK compatibility

The runtime release workflow builds the SDK workspace at the same tagged commit,
embeds its default plugin WASM artifacts, and runs the IIR contract corpus. That
is the release gate for the binary and the default plugins shipped inside it.

SDK packages may release independently. Their release notes must name the CE
version tested whenever they change plugin manifests, WASM exports, host
functions, grammar loading, source lifting, or default-plugin behavior.

External clients such as Studio remain compatibility-managed. Changes affecting
their REST, WebSocket, or MCP assumptions need contract tests and a
`Compatibility` release note stating the versions tested or required.

## Release notes

Use these headings when relevant:

- `Runtime` — CLI, storage, server, MCP/API, indexing, and runner behavior.
- `Plugin SDK` — SDK APIs, sandbox, scaffolding, and default plugins.
- `Studio` — UI, graph/query/trace flows, and API assumptions.
- `Compatibility` — tested versions, required upgrades, migrations, and known
  gaps.

## Local development

The SDK workspace is already part of this checkout:

```bash
git clone https://github.com/atheory-ai/context-engine.git
cd context-engine

pnpm --dir sdk install --frozen-lockfile
pnpm --dir sdk build
make bundle-default-plugins
go build -o ce ./cmd/ce
```

`make bundle-default-plugins` stages the matching default plugin artifacts for
embedding in a local CE binary. The release workflow runs the same operation
before it builds the archives.

To test Studio against a local CE build, start CE and then use the Studio
repository's documented development flow:

```bash
CE_DATA_DIR=/tmp/ce-dev ./ce server start
```

## Release checklist

For a CE runtime release:

1. Update `VERSION`, `CHANGELOG.md`, and any compatibility notes.
2. Run `make verify`, `sh -n install.sh`, and `make release-snapshot`.
3. Tag the `main` commit as `vX.Y.Z` and monitor the GitHub Release workflow.
4. Smoke-test the published curl installer against the tagged version.
5. Record external client compatibility in the release notes where applicable.
