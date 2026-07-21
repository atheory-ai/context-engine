# Changelog

All notable changes to this project should be documented in this file.

The format is based on Keep a Changelog and the project uses Semantic Versioning.

## [Unreleased]

## [0.4.0]

### Runtime

- Reworked indexing into bounded parse and extraction stages. Source plus CST
  admission is now byte-budgeted, with independently configurable parse and
  extraction worker pools.
- Persist content-addressed source and compact CST artifacts, allowing later
  host-side analysis to reuse the initial parse without retaining the corpus in
  process memory.
- Stage file output in SQLite as each file completes, then reconcile it as one
  replacement-safe index run. Completed work flushes in bounded batches rather
  than accumulating until the end of a large corpus.
- Run independent same-file plugins concurrently according to manifest
  capabilities while preserving declared dependency layers and legacy plugin
  ordering. Each file still merges and validates all plugin output before it is
  staged.
- Report source, serialized CST, and estimated plugin-input byte totals in the
  CLI index summary to aid worker and byte-budget tuning.

### Upgrade

- The graph migration adds durable source/CST artifact and staged-file tables
  automatically. Run `ce index --full` after upgrading so existing content is
  recorded with the new artifact and replacement metadata.

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
