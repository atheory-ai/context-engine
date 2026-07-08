import type { ConceptSeed } from "@atheory-ai/ce-plugin-sdk"

export const tsConceptSeeds: ConceptSeed[] = [
  {
    term:       "component",
    definition: "React function or class component that returns JSX",
    synonyms:   ["react-component", "ui-component"],
    related:    ["props", "hooks", "jsx"],
  },
  {
    term:       "hook",
    definition: "React hook — function starting with 'use' that manages state or side effects",
    synonyms:   ["react-hook", "custom-hook"],
    related:    ["useState", "useEffect", "useCallback"],
  },
  {
    term:       "interface",
    definition: "TypeScript interface — structural contract for object shapes",
    synonyms:   ["type-contract", "shape"],
    related:    ["type-alias", "generics", "duck-typing"],
  },
  {
    term:       "module",
    definition: "ES module — a file with its own scope exporting and importing symbols",
    synonyms:   ["es-module", "esm"],
    related:    ["import", "export", "bundler"],
  },
  {
    term:       "async-await",
    definition: "Asynchronous programming pattern using async functions and await expressions",
    synonyms:   ["async", "await", "promise-chain"],
    related:    ["promise", "fetch", "error-handling"],
  },
  {
    term:       "generics",
    definition: "TypeScript generic types for reusable, type-safe abstractions",
    synonyms:   ["type-parameter", "parameterized-type"],
    related:    ["interface", "type-alias", "constraint"],
  },
  {
    term:       "server-component",
    definition: "Next.js React Server Component — renders on the server, no client JS",
    synonyms:   ["rsc", "server-side"],
    related:    ["client-component", "data-fetching", "streaming"],
  },
  {
    term:       "middleware",
    definition: "Function that intercepts and transforms requests in a pipeline",
    synonyms:   ["interceptor", "handler-chain"],
    related:    ["next-middleware", "express-middleware", "decorator"],
  },
]
