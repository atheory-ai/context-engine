# @atheory-ai/create-ce-plugin

Interactive scaffolding for Context Engine WebAssembly plugin projects.

## Create a plugin

```bash
pnpm create @atheory-ai/ce-plugin
```

The generator creates a TypeScript project with a CE manifest, starter source,
fixtures, tests, a supported WASM-toolkit build configuration, and ESLint
flat-config setup.

## Build the generated plugin

```bash
cd my-plugin
pnpm install
pnpm build
ce plugin validate dist/my-plugin.wasm
```

## License

Apache-2.0
