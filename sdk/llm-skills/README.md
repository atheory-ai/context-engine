# LLM Skills — Context Engine Plugin Authoring

This directory contains structured markdown files for LLMs to use as context
when generating or reviewing Context Engine plugins.

## How to use

Include the relevant files as context when prompting an LLM:

```
You are writing a Context Engine plugin. Read these files first:
- plugin-architecture.md  — the definePlugin contract
- extraction-patterns.md  — patterns for your language type
- tool-design.md          — if adding tools
- anti-patterns.md        — common mistakes to avoid
- validation-checklist.md — before finishing
```

## Files

| File | When to include |
|------|-----------------|
| `plugin-architecture.md` | Always |
| `extraction-patterns.md` | When writing language.extract() |
| `concept-design.md` | When designing concept seeds |
| `tool-design.md` | When writing tools |
| `validation-checklist.md` | Before shipping |
| `anti-patterns.md` | When debugging extraction failures |
| `worked-examples.md` | When starting from scratch |
| `sandbox-workflow.md` | When iterating on coverage |
