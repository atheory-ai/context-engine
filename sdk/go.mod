// This module walls off the SDK subtree from the parent Go module so that
// `go build ./...` at the repo root does not try to compile the TypeScript
// plugins' Go test fixtures. The SDK is a pnpm/TypeScript workspace; it has no
// real Go code — only parser-input fixtures under plugins/*/fixtures.
module atheory.ai/ce-sdk

go 1.24
