# Changelog

All notable changes to this project should be documented in this file.

The format is based on Keep a Changelog and the project uses Semantic Versioning.

## [Unreleased]

- Corrected release binaries so `ce version` reports the tagged version.

## [0.2.0]

### Runtime

- Made successful indexing runs replacement-safe: file output ownership now
  removes stale nodes, edges, and extracted IIR when source positions move or
  disappear.
- Added bounded write-buffer backpressure and held index commits so partial
  indexing output is not made visible after an extraction or write failure.
- Added canonical host-owned file anchors, deterministic same-file contribution
  merging, and stale org-graph projection replacement.

### Plugin SDK

- Added plugin composition contracts (`provides`, `requires`, and `enriches`)
  plus an optional host source anchor for language extraction.

### Upgrade

- After upgrading, run `ce index --full` once to replace legacy plugin output
  with tracked file contributions.

- Documented the semantic-platform north star, current IIR capability matrix,
  plugin source-lift contract, historical RFC supersession, and the release
  gate that tests CE against matching default plugins.
- Initial open source project scaffolding for contribution, security, and release process documentation.
- Added release compatibility documentation covering aligned sibling repo versioning, release note expectations, compatibility matrix, and local dev linking.
- Established GitHub Releases and the checksum-verifying curl installer as the
  first public CE distribution channel. Homebrew and an npm wrapper are planned
  follow-on channels; npm will not duplicate native binaries.
