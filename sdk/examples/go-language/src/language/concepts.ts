import type { ConceptSeed } from "@atheory-ai/ce-plugin-sdk"

export const concepts: ConceptSeed[] = [
  { term: "goroutine",    definition: "Lightweight concurrent thread managed by the Go runtime" },
  { term: "channel",      definition: "Typed conduit for communication between goroutines" },
  { term: "interface",    definition: "Named collection of method signatures" },
  { term: "struct",       definition: "Composite data type with named fields" },
  { term: "receiver",     definition: "Type a method is attached to in Go" },
  { term: "package",      definition: "Compilation unit and namespace in Go" },
  { term: "error-type",   definition: "Go error interface implementation" },
  { term: "context-type", definition: "context.Context — deadline/cancellation propagation" },
  { term: "middleware",   definition: "Function wrapping another function for cross-cutting concerns" },
]
