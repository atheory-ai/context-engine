# Changelog

All notable changes to this project should be documented in this file.

The format is based on Keep a Changelog and the project uses Semantic Versioning.

## [Unreleased]

## [0.6.0]

### Semantic preparation

- Added `ce iir prepare`, which shapes a declared or natural-language request
  into an evidence-backed implementation packet for an LLM or harness agent.
  CE deliberately does not generate source at this boundary.
- Added a controlled semantic-tag vocabulary for operation, framework context,
  trust boundary, presentation, and effect facts. Model-proposed tags are
  validated and recorded as inferred; caller-supplied tags are recorded as
  declared rather than silently treated as observed code facts.
- Added deterministic host-side decoration of semantic plans with active
  plugin policy packs. The host owns validation, selection, ordering,
  conflict handling, provenance, and packet construction; plugin WASM never
  receives arbitrary plan-mutation authority.
- Added conjunctive `allClaimKinds` policy selectors, so framework security and
  quality rules activate only for the intended combination of semantic facts.

### IIR and model routing

- Corrected failure comparison: absent comparable failure evidence is now
  inconclusive, while an observed conflicting failure code is a failure.
- Added configurable OpenAI reasoning effort and correct standard-tier model
  routing for direct semantic shaping.

### Plugin SDK

- Added typed, versioned `semanticPolicies` manifest support, including
  `allClaimKinds` selectors and build-time validation.
- Corrected the project-local Extism compiler bootstrap on macOS by preserving
  Binaryen's required dynamic library directory.

### Compatibility

- CE v0.6.0 is tested with Plugin SDK v0.5.0. Rebuild policy-contributing
  plugins with SDK v0.5.0 so their compiled manifest sidecars include their
  semantic policy packs.

## [0.5.0]

### Indexing

- Replaced the plugin extraction transport's JSON CST with ABI-v4 compact
  binary node tables. Source travels once as bytes; node text is recovered
  lazily from byte ranges rather than duplicated at every CST node.
- Production Extism plugins consume raw input bytes and a cached SDK node-view
  adapter, eliminating full-payload `JSON.parse` and repeated tree-wrapper
  allocation during multi-pass extraction.
- Reduced compact-CST admission sizing to match the new transport while
  preserving the byte-budget backpressure boundary.
- Added index profiling output and production-plugin golden coverage for the
  compact binary transport.

### Plugin SDK

- Released Plugin SDK ABI v4. Production and development adapters now decode
  the same compact extraction envelope; compiled v3 artifacts are rejected
  with a rebuild instruction.

### Upgrade

- Rebuild every language or convention plugin with
  `@atheory-ai/ce-plugin-sdk` v0.4.0 before using CE v0.5.0. ABI-v3 artifacts
  are intentionally not accepted by the v4 host.

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
