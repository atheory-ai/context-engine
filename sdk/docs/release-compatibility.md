# SDK Release Compatibility

The SDK workspace is part of the Context Engine repository. Its npm packages,
the CE runtime, and CE Studio are versioned independently; compatibility is an
explicit release claim rather than a shared version number.

| Component | Version source | Published artifact |
| --- | --- | --- |
| Context Engine | Git tag `vX.Y.Z` | `ce` binaries and archives |
| Plugin SDK | Package version and `plugin-sdk-vX.Y.Z` tag | `@atheory-ai/ce-plugin-sdk` |
| Plugin Sandbox | Package version and `plugin-sandbox-vX.Y.Z` tag | `@atheory-ai/ce-plugin-sandbox` |
| Create CE Plugin | Package version and `create-ce-plugin-vX.Y.Z` tag | `@atheory-ai/create-ce-plugin` |
| CE Studio | Studio package version and tag | Studio npm package and release bundle |

SDK release notes must name the CE version tested when a change affects plugin
manifests, WASM exports, host functions, tree-sitter grammar loading, or
default-plugin behavior. Name the tested Studio version when API assumptions
are affected.

Before release, run `pnpm release:check` and `pnpm release:dry-run` from
`sdk/`, and include compatibility or migration notes in `sdk/CHANGELOG.md`.
