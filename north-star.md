# Compiling Intent

## North star for a semantic software development platform

> Software generation should not compile directly from natural language to
> source code. It should compile through a semantic intermediate representation
> that can be analyzed, transformed, enriched, constrained, and verified before
> code is generated.

## The problem

Modern AI coding systems put an increasingly fragile collection of concerns
inside one model invocation: user intent, architecture, conventions, policy,
implementation choices, correctness, verification, and repair. They address
growing complexity chiefly by adding more prompt context, retrieval, examples,
skills, and tool loops.

That architecture scales context rather than reducing uncertainty.

## A different architecture

Like a compiler, software generation should lower through representations that
can be analyzed and transformed before source is emitted:

```text
Natural language
        |
        v
Intermediate Intent Representation (IIR)
        |
        +------------------+
        |                  |
        v                  v
semantic compiler passes   project knowledge
        |                  |
        +--------+---------+
                 v
resolved semantic plan
        v
implementation recipe
        v
LLM code generation
        v
source code -> AST -> semantic analysis -> observed IIR
        |                                      |
        +------------ semantic verification ---+
```

The LLM remains essential, but it should not carry the entire development
process in a single context window.

## IIR

IIR is not another AST. The AST preserves syntax and implementation detail;
IIR preserves the semantic claims that matter for planning, generation, and
verification.

It should express operations, effects, constraints, failures, invariants,
obligations, ownership, dependencies, and architectural intent. It does not
need to describe every line of code—only what the software means.

The semantic layer references the AST; it does not replace it:

```text
source code -> AST -> semantic analysis -> IIR claims
```

## Planning as compilation

Planning should compile prose into a structured semantic form. A planning
compiler asks:

- What operation is this?
- What are its inputs, outputs, failures, effects, and invariants?
- Which project policies and symbols apply?
- Which decisions remain unresolved?

The representation itself makes ambiguity visible before generation begins.

## Semantic compiler passes and plugins

Semantic passes transform or enrich intent before code generation. They may
apply architectural policy, resolve symbols, insert observability obligations,
enforce error models, normalize implementation patterns, or apply organization
standards.

Plugins are semantic compiler passes, not merely extensions. They let teams
encode durable architecture, security, observability, domain, implementation,
and verification policies as executable transformations and checks rather than
repeating them in prompts.

Examples:

- External effects require logging.
- Domain services never throw.
- Mutations emit audit events.
- Repositories are mandatory boundaries.
- Provider failures must be wrapped.

## Fidelity and conformance

Verification compares semantic intent rather than source text:

```text
declared intent -> expected IIR
observed code  -> observed IIR

expected IIR <-> observed IIR
```

**Fidelity** asks whether the implementation preserves the intended semantic
claims. **Conformance** asks whether a faithful implementation follows the
project's policies and preferred patterns. The two gates are separate and both
must produce actionable, localized results.

## Generation and repair

The goal is not deterministic source code; it is semantic invariance. Naming,
formatting, helpers, local structure, and idiom may vary when the required
effects, prohibitions, relationships, and behavior remain true.

Before generation, resolve ambiguity and apply policy to produce an
implementation recipe. The model then realizes that constrained recipe in the
target language. After generation, lift source back to observed IIR and verify
it. Repair targets the semantic mismatch rather than relying on textual drift.

## Context becomes resolution

Architecture should be resolved into semantic data, not repeatedly carried as
prompt text. Context becomes symbol resolution, architectural knowledge, policy
application, and semantic enrichment.

This lowers the number of architectural decisions the model must invent while
emitting tokens.

## Semantic build graph

Treat software as semantic units—not only files. Each operation, function, or
responsibility can carry its intended semantics, resolved dependencies,
implementation recipe, generated source, observed semantics, and verification
results. This enables targeted generation, repair, search, review, testing,
refactoring, impact analysis, and multi-agent coordination.

## Current footing and direction

Context Engine already has an initial IIR foundation: plugin-owned source lifts,
stored observed function intent, intent comparison, conformance rules, repair,
and CLI/MCP/API entry points.

The north-star work is to grow that foundation into resolved semantic plans and
implementation recipes; richer semantic units and relationships; policy passes
that enrich intent before generation; and generation whose output is accepted
because it round-trips semantically, not because it matches a source template.

The result should be a programmable semantic substrate shared by generation,
planning, verification, search, review, documentation, testing, refactoring,
and coordination—not another isolated coding assistant.
