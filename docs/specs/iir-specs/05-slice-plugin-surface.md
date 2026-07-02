# Slice 5: Plugin Surface

## Goal

Define the plugin surface that will let teams extend IIR extraction, rule evaluation, comparison, and generation later.

This slice may be interface-first. It does not need a dynamic runtime yet.

## Plugin responsibilities

Plugins may contribute:

- extractors
- analyzers
- IIR node types
- rule packs
- comparison strategies
- code emitters
- test emitters
- renderers

## MVP plugin interfaces

```ts
export interface IirPlugin {
  id: string;
  name: string;
  version: string;
  languages?: string[];
  extractors?: IirExtractor[];
  rules?: RulePack[];
  comparators?: IirComparator[];
}

export interface IirExtractor {
  id: string;
  supports(input: ExtractionInput): boolean;
  extract(input: ExtractionInput, context: ExtractionContext): Promise<ExtractionResult>;
}

export interface IirComparator {
  id: string;
  supports(intended: IirNode, extracted: IirNode): boolean;
  compare(intended: IirNode, extracted: IirNode, context: ComparisonContext): Promise<ComparisonResult>;
}
```

## In scope

- Define plugin manifest shape.
- Define TypeScript interfaces.
- Allow built-in TypeScript function extractor to behave like a plugin internally.
- Allow built-in rule pack loading.
- Document extension points.

## Out of scope

- WASM execution
- remote plugins
- marketplace
- sandboxing
- hot reloading
- plugin publishing

## Acceptance criteria

- Built-in extractor uses the same interface future plugins will use.
- Built-in comparator uses the same interface future plugins will use.
- Rule packs can be associated with a plugin id.
- Plugin contracts are documented.
