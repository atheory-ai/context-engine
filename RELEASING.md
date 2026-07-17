# Releasing Context Engine

This document is for maintainers.

## Distribution model

GitHub Releases are the canonical CE distribution. A release contains signed
native archives for macOS, Linux, and Windows on amd64 and arm64, plus checksums,
an SPDX SBOM, and build provenance. The public `install.sh` installer resolves
one of those archives, verifies its checksum, and installs `ce`.

Distribution channels are introduced in this order:

1. **GitHub Releases and curl installer** — current release path.
2. **Homebrew** — planned; its formula will consume the same GitHub archives.
3. **npm wrapper** — planned; it will resolve the matching GitHub Release
   binary and will not publish duplicate platform binaries.

The GitHub Release must succeed independently of Homebrew or npm. Do not add a
downstream publisher as a prerequisite of the canonical binary release.

## Release model

- `main` is the release branch.
- Runtime releases are tags in the form `vX.Y.Z` on commits reachable from
  `main`; the tag must match the root [VERSION](./VERSION) file.
- The [release workflow](./.github/workflows/release.yml) verifies CE, builds
  the in-tree SDK default plugins, runs the IIR contract gate, builds cross-
  platform archives with GoReleaser, signs them with keyless OIDC, generates an
  SBOM and provenance, and creates the GitHub Release.
- The SDK workspace has its own package-level npm release process. See
  [sdk/RELEASING.md](./sdk/RELEASING.md).
- External clients such as Studio are compatibility-managed, but they do not
  block the runtime artifact release. State the tested versions and any upgrade
  requirements in the release notes.

## GitHub configuration

The workflow needs only the repository-provided `GITHUB_TOKEN` and the
permissions declared in the workflow:

- `contents: write` to create the release and upload its assets;
- `id-token: write` for keyless Sigstore signing and GitHub provenance.
- `attestations: write` to persist the provenance attestation.

There is no npm trusted-publisher configuration, `NPM_TOKEN`, or Homebrew tap
token required for this release path. Before the first public release, make the
repository public so `install.sh` and release assets are accessible without a
GitHub credential. Artifact attestations are available to public repositories
on GitHub Free, Pro, and Team; private repositories require GitHub Enterprise
Cloud.

## Release steps

1. Merge the intended changes to `main`.
2. Update [VERSION](./VERSION), [CHANGELOG.md](./CHANGELOG.md), and any
   compatibility notes.
3. Validate locally:

   ```bash
   git checkout main
   git pull --ff-only
   make verify
   sh -n install.sh
   make release-snapshot
   ```

4. Create and push the matching tag:

   ```bash
   git tag vX.Y.Z
   git push origin vX.Y.Z
   ```

5. Monitor the `Release` workflow. It must finish with a GitHub Release
   containing six platform archives, `checksums.txt`, signature bundles, an
   SPDX SBOM, provenance, and the aggregate `ce-X.Y.Z-dist.tar.gz` bundle.
6. Smoke-test the released installer in a clean directory:

   ```bash
   install_dir="$(mktemp -d)"
   curl -fsSL https://raw.githubusercontent.com/atheory-ai/context-engine/main/install.sh \
     | CE_VERSION=X.Y.Z CE_INSTALL_DIR="$install_dir" sh
   "$install_dir/ce" version
   ```

## Follow-on channels

Homebrew and npm are intentionally not part of the tag workflow yet. Add either
only after it consumes and verifies the released archives, has its own
regression coverage, and cannot prevent the GitHub Release from being created.
