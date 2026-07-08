# Designing Concept Seeds

## What are concept seeds?

Concept seeds are domain vocabulary terms contributed to the Strategizer.
They help the engine understand what topics are relevant to this language/framework.

## Format rules

```typescript
// CORRECT
{ term: "goroutine", definition: "Lightweight concurrent thread managed by the Go runtime" }
{ term: "dependency-injection", definition: "Pattern for providing dependencies from outside" }

// WRONG — uppercase
{ term: "Goroutine", ... }
// WRONG — spaces
{ term: "dependency injection", ... }
// WRONG — camelCase
{ term: "dependencyInjection", ... }
```

The term MUST match `/^[a-z][a-z0-9-]*$/`.

## Good concept seeds

1. **Language-specific primitives** — concepts the language introduces that
   don't exist elsewhere (`goroutine`, `chan`, `defer`)

2. **Design patterns used in this ecosystem** — `middleware`, `decorator`,
   `dependency-injection`, `repository-pattern`

3. **Framework-specific terms** — `component`, `hook`, `provider` (React),
   `service`, `controller`, `module` (Angular/NestJS)

4. **Error handling patterns** — `error-wrapping`, `panic-recovery`, `sentinel-error`

## Optional fields

```typescript
{
  term:       "middleware",
  definition: "Function wrapping another function for cross-cutting concerns",
  related:    ["decorator", "interceptor", "handler-chain"],
  synonyms:   ["interceptor"]
}
```

`related` links to other terms in the vocabulary.
`synonyms` tells the engine these terms should activate each other.

## How many seeds?

5-20 seeds is typical. More is fine for rich ecosystems.
Focus on terms that:
- Appear frequently in code written in this language
- Would help someone understand what a codebase does
- Are NOT generic terms (don't seed "function", "class", "variable")
