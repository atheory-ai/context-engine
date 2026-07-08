# Releasing CE Plugin SDK

This document is for maintainers.

## Release Model

- `main` is the release branch.
- Releases are created from a tagged commit on `main`.
- Tags should follow `vX.Y.Z`.
- Coordinated releases use the same `vX.Y.Z` train across `context-engine`, `ce-plugin-sdk`, and `atheory-ce-studio`; see [Release Compatibility](./docs/release-compatibility.md).
- Published package versioning and distribution should stay consistent with the root release process.
- The npm packages are `@atheory-ai/ce-plugin-sdk`, `@atheory-ai/ce-plugin-sandbox`, and `@atheory-ai/create-ce-plugin`.
- The release workflow in [`.github/workflows/release.yml`](./.github/workflows/release.yml) validates the workspace, publishes npm packages, bundles built artifacts, and publishes a GitHub release.

## Release Steps

1. Make sure the intended release changes are merged to `main`.
2. Update [CHANGELOG.md](./CHANGELOG.md).
3. Review any package version changes that will be published.
4. Confirm [Release Compatibility](./docs/release-compatibility.md) reflects the sibling versions tested for this release.
5. Confirm the repository has an `NPM_TOKEN` secret with publish access for the `@atheory-ai` npm scope.
6. Validate the workspace locally:

```bash
pnpm install --frozen-lockfile
pnpm lint
pnpm build
pnpm test
```

7. Create and push the release tag:

```bash
git checkout main
git pull --ff-only
git tag vX.Y.Z
git push origin vX.Y.Z
```

8. Monitor the GitHub Actions release workflow and confirm the npm packages and generated artifact bundle were published.

## Notes

- If a release changes the generated plugin scaffold or default plugin behavior, call that out explicitly in the release notes.
- Use the `Compatibility` release note heading when SDK changes require a matching CE or Studio version.
- Package publishing runs through the `npm-release` environment.
