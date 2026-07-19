# Releasing CE Plugin SDK packages

The `sdk/` workspace in Context Engine publishes three independent npm packages:

- `@atheory-ai/ce-plugin-sdk`
- `@atheory-ai/ce-plugin-sandbox`
- `@atheory-ai/create-ce-plugin`

Default language plugins are source and CE release-bundle artifacts, not npm
packages.

## Release model

- SDK packages are versioned independently.
- Release tags identify one package and version:
  - `plugin-sdk-vX.Y.Z`
  - `plugin-sandbox-vX.Y.Z`
  - `create-ce-plugin-vX.Y.Z`
- Tags must point to a commit reachable from `main`.
- The `npm-release` environment gates publication.
- npm trusted publishing uses GitHub Actions OIDC; the workflow does not use
  `NPM_TOKEN`.

Configure one npm trusted publisher for each package, pointing at
`.github/workflows/plugin-sdk-release.yml` in `atheory-ai/context-engine` and
requiring the `npm-release` environment.

## Local verification

```bash
cd sdk
pnpm install --frozen-lockfile
pnpm release:check
pnpm release:dry-run
```

Inspect one package with:

```bash
pnpm release:dry-run -- --package plugin-sdk
```

## Publish

1. Update the affected package version and `sdk/CHANGELOG.md`.
2. Record the CE version tested if the change alters manifests, WASM exports,
   host bindings, grammar loading, or default plugin behavior.
3. Run the local verification commands and inspect the dry run.
4. Merge to `main`, then tag that commit, for example:

   ```bash
   git tag plugin-sdk-v0.2.0
   git push origin plugin-sdk-v0.2.0
   ```

5. Approve the `npm-release` environment and confirm npm provenance.

Publishing runs only from GitHub Actions; do not publish from a workstation.
