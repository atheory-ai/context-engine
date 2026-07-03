# Context Engine IIR Feature Overview

## Purpose

Add an Intermediate Intent Representation (IIR) capability to Context Engine.

IIR is a structured representation of what code is intended to do. It sits above ASTs and semantic analyzers, but below natural language and agent workflows.

The first version should prove this loop:

```text
declared intent
→ source code
→ extracted intent
→ comparison
→ verification report
```

## Core thesis

Agents are strongest when converting one representation into another and checking whether the result preserves meaning.

IIR gives code work a representation that is:

- more semantic than AST
- more verifiable than prose
- more compact than raw source
- more durable than prompt instructions
- useful for both code generation and test generation

## Product boundaries

This belongs inside Context Engine as a semantic capability.

The coding harness can later use this capability to coordinate agents, prompt models, generate code, generate tests, and repair mismatches.

## Roles

### Context Engine

Responsible for:

- parsing source files
- extracting ASTs
- running semantic analyzers
- producing IIR
- running rules over IIR
- comparing intended IIR against extracted IIR
- producing verification reports

### IIR plugins

Responsible for:

- framework-specific semantic extraction
- language-specific idioms
- team-specific coding rules
- architectural constraints
- code generation emitters
- test generation strategies

### Skills

Skills are only for procedures.

They should describe workflows such as release, migration, CI triage, review, or deployment.

Durable code semantics should live in IIR rules and plugins, not skills.

## MVP principle

Start with verification, not generation.

The first useful capability is proving that Context Engine can determine whether code matches declared intent.
