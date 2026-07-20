# Changelog

All notable changes to this project should be documented in this file.

The format is based on Keep a Changelog and the project uses Semantic Versioning.

## [Unreleased]

## [0.3.0]

### Runtime

- Fixed index writes so completed file contributions flush in bounded SQLite
  WAL batches during a run, rather than accumulating in memory until its end.
- Stage index-managed nodes and edges until a successful run promotes them,
  preserving the prior complete graph if indexing fails.
- Unified the CLI and MCP server version metadata; release builds now report
  the tagged CE version consistently.

### Upgrade

- The graph migration adds internal index-staging tables automatically. Re-run
  `ce index --full` after upgrading to replace output from interrupted runs.

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
