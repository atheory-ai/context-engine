# Releasing Context Engine

This document is for maintainers.

## Release Model

- `main` is the release branch.
- Releases are created from a tagged commit on `main`.
- Tags should follow `vX.Y.Z`.
- Release artifacts are built with GoReleaser using [`.goreleaser.yaml`](./.goreleaser.yaml).
- [`.github/workflows/release.yml`](./.github/workflows/release.yml) validates the tag, builds the release bundle, and publishes the GitHub release.

## Release Steps

1. Make sure the intended release changes are merged to `main`.
2. Update [CHANGELOG.md](./CHANGELOG.md).
3. Pull the latest `main` branch locally.
4. Validate the release build:

```bash
git checkout main
git pull --ff-only
make release-snapshot
```

1. Create and push the release tag:

```bash
git tag vX.Y.Z
git push origin vX.Y.Z
```

1. Monitor the GitHub Actions release workflow and confirm the generated release bundle was attached to the release.

## Notes

- The current GoReleaser configuration builds cross-platform archives and Homebrew metadata.
- If you change packaging, Homebrew metadata, or binary naming, update both [`.goreleaser.yaml`](./.goreleaser.yaml) and [README.md](./README.md) as needed.
