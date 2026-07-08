# @atheory-ai/ce-plugin-sdk

TypeScript SDK for authoring Context Engine plugins.

## Installation

```bash
pnpm add @atheory-ai/ce-plugin-sdk
```

## Usage

```typescript
import { definePlugin, nodeID, edgeID } from "@atheory-ai/ce-plugin-sdk"

export default definePlugin({
  id:      "com.example.my-plugin",
  name:    "My Plugin",
  version: "1.0.0",

  language: {
    match:   (filePath) => filePath.endsWith(".myext"),
    extract: (filePath, content) => ({
      nodes: [/* ... */],
      edges: [/* ... */],
    }),
  },
})
```

## Key Constraints

- No Node.js APIs (`fs`, `path`, `process`) — plugins run in a WASM sandbox
- Always use `nodeID()` and `edgeID()` — never construct IDs manually
- Tool `description` must be ≤ 100 characters
- Concept `term` must be `lowercase-hyphenated`
- `activate()` must be a pure function

## ESLint Plugin

The SDK ships an ESLint plugin that enforces these constraints at lint time:

```json
{
  "extends": ["@atheory-ai/ce-plugin-sdk/eslint-plugin-ce"]
}
```
