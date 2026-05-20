# Releasing Context Engine

This document is for maintainers.

## Release Model

- `main` is the release branch.
- Releases are created from a tagged commit on `main`.
- Tags should follow `vX.Y.Z`.
- Coordinated releases use the same `vX.Y.Z` train across `context-engine`, `ce-plugin-sdk`, and `atheory-ce-studio`; see [Release Compatibility](./docs/release-compatibility.md).
- The tag must match the root [VERSION](./VERSION) file.
- Release artifacts are built with GoReleaser using [`.goreleaser.yaml`](./.goreleaser.yaml).
- [`.github/workflows/release.yml`](./.github/workflows/release.yml) validates the tag, checks SDK and Studio compatibility, builds the release bundle, publishes npm packages, and publishes the GitHub release.

## Release Steps

1. Make sure the intended release changes are merged to `main`.
2. Update [VERSION](./VERSION).
3. Update [CHANGELOG.md](./CHANGELOG.md).
4. Confirm [Release Compatibility](./docs/release-compatibility.md) reflects the sibling versions tested for this release.
5. Confirm the repository has an `NPM_TOKEN` secret with publish access for the `@atheory-ai` npm scope.
6. Pull the latest `main` branch locally.
7. Validate the release build:

```bash
git checkout main
git pull --ff-only
make release-snapshot
```

8. Create and push the release tag:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

9. Monitor the GitHub Actions release workflow and confirm the generated release bundle and npm packages were published.

## Notes

- The current GoReleaser configuration builds darwin, linux, and windows archives for amd64 and arm64 with Zig-backed CGO cross-compilation, plus Homebrew metadata.
- The release workflow gates publishing on the current `atheory-ai/ce-plugin-sdk` and `atheory-ai/atheory-ce-studio` verification commands so CE does not ship against broken sibling repos.
- The npm package `@atheory-ai/ce` is a Node wrapper around platform-specific native binary packages.
- Use the `Compatibility` release note heading when a release requires a matching SDK or Studio version.
- If you change packaging, Homebrew metadata, or binary naming, update both [`.goreleaser.yaml`](./.goreleaser.yaml) and [README.md](./README.md) as needed.
