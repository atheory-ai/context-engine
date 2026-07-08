import type { ConceptSeed } from "@atheory-ai/ce-plugin-sdk"

export const goConceptSeeds: ConceptSeed[] = [
  {
    term:       "error-handling",
    definition: "Go's explicit error return pattern",
    synonyms:   ["error", "err", "errors"],
    related:    ["error-wrapping", "sentinel-error"],
  },
  {
    term:       "interface",
    definition: "Go implicit interface satisfaction — any type with the right method set satisfies it",
    synonyms:   ["contract", "protocol"],
    related:    ["implementation", "duck-typing", "method-set"],
  },
  {
    term:       "goroutine",
    definition: "Lightweight concurrent execution unit managed by the Go runtime",
    synonyms:   ["go-routine", "concurrent"],
    related:    ["channel", "sync", "waitgroup"],
  },
  {
    term:       "channel",
    definition: "Go typed message-passing primitive for goroutine communication",
    synonyms:   ["chan"],
    related:    ["goroutine", "select", "concurrency"],
  },
  {
    term:       "context",
    definition: "Go context.Context for cancellation and deadline propagation",
    synonyms:   ["ctx"],
    related:    ["cancellation", "deadline", "timeout"],
  },
  {
    term:       "middleware",
    definition: "HTTP handler wrapping pattern for cross-cutting concerns",
    synonyms:   ["handler-chain", "interceptor"],
    related:    ["http-handler", "router"],
  },
  {
    term:       "struct",
    definition: "Composite data type with named fields",
    synonyms:   ["record", "data-type"],
    related:    ["method", "embedding", "interface"],
  },
  {
    term:       "receiver",
    definition: "The type a method is attached to in Go",
    synonyms:   ["method-receiver"],
    related:    ["method", "struct", "pointer-receiver"],
  },
  {
    term:       "embedding",
    definition: "Composition via anonymous fields — Go's alternative to inheritance",
    synonyms:   ["anonymous-field", "promotion"],
    related:    ["struct", "interface", "composition"],
  },
  {
    term:       "defer",
    definition: "Deferred function call executed when surrounding function returns",
    synonyms:   ["cleanup"],
    related:    ["panic", "recover", "resource-management"],
  },
]
