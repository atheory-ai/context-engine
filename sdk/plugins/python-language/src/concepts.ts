import type { ConceptSeed } from "@atheory-ai/ce-plugin-sdk"

export const pythonConceptSeeds: ConceptSeed[] = [
  {
    term:       "class",
    definition: "Python class definition — blueprint for creating objects",
    synonyms:   ["class", "object", "oop"],
  },
  {
    term:       "dataclass",
    definition: "Python @dataclass — auto-generated __init__, __repr__, etc.",
    synonyms:   ["dataclass", "data class"],
    related:    ["class"],
  },
  {
    term:       "decorator",
    definition: "Python decorator — higher-order function wrapping another function or class",
    synonyms:   ["decorator", "wrapper", "@"],
  },
  {
    term:       "generator",
    definition: "Python generator — function using yield to lazily produce values",
    synonyms:   ["generator", "yield", "iterator"],
    related:    ["async"],
  },
  {
    term:       "async",
    definition: "Python async/await — coroutines using asyncio event loop",
    synonyms:   ["async", "await", "asyncio", "coroutine"],
  },
  {
    term:       "context-manager",
    definition: "Python context manager — __enter__/__exit__ protocol for resource management",
    synonyms:   ["context manager", "with statement", "contextmanager"],
    related:    ["class"],
  },
  {
    term:       "property",
    definition: "Python @property — computed attribute descriptor",
    synonyms:   ["property", "getter", "setter"],
    related:    ["decorator"],
  },
  {
    term:       "abstract",
    definition: "Abstract base class — ABC or ABCMeta defining required interface",
    synonyms:   ["abstract", "ABC", "interface", "protocol"],
    related:    ["class"],
  },
  {
    term:       "exception",
    definition: "Custom exception class — subclass of Exception or BaseException",
    synonyms:   ["exception", "error", "raise", "try"],
    related:    ["class"],
  },
  {
    term:       "type-hint",
    definition: "Python type annotation — PEP 484/526 static type hints",
    synonyms:   ["type hint", "annotation", "typing"],
  },
]
