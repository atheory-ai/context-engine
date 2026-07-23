# Changelog

All notable changes to this project should be documented in this file.

The format is based on Keep a Changelog and the project uses Semantic Versioning.

## [Unreleased]

## [0.5.0]

### Added

- Typed, versioned `semanticPolicies` plugin manifest contributions. CE
  evaluates these declarative packs on the host, retaining plugin provenance
  for every resulting implementation requirement.
- `allClaimKinds` selectors for policies that require an explicit conjunction
  of semantic facts.

### Fixed

- The project-local Extism compiler bootstrap now preserves Binaryen's macOS
  dynamic library directory, allowing fresh local plugin builds without a
  global compiler installation.

### Compatibility

- Tested with Context Engine v0.6.0. Rebuild policy-contributing plugins with
  this SDK release so production ABI sidecar manifests carry
  `semanticPolicies`.

- Corrected published package metadata to the in-tree SDK workspace in
  `atheory-ai/context-engine` for the next `0.1.1` metadata patch releases.
- Initial open source project scaffolding for contribution, security, and release process documentation.
- Added release compatibility documentation covering aligned sibling repo versioning, release note expectations, compatibility matrix, and local dev linking.
- Added release workflow validation that package versions match the release tag.
- Renamed published SDK packages under the `@atheory-ai` npm scope and added npm publishing automation.
