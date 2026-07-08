import * as esbuild from "esbuild"

// Mark @atheory-ai/wasm-plugin-toolkit as external — consumers of
// `@atheory-ai/ce-plugin-sdk/build/abi` install the toolkit themselves
// (it's a peerDep) and the AbiModule type is type-only at runtime.
const external = ["@atheory-ai/wasm-plugin-toolkit"]

// ESM build of the main entry.
await esbuild.build({
  entryPoints: ["src/index.ts", "src/abi.ts"],
  bundle:      true,
  format:      "esm",
  outdir:      "dist",
  target:      "es2020",
  platform:    "neutral",
  sourcemap:   true,
  external,
})

// CJS build of the main entry.
await esbuild.build({
  entryPoints: ["src/index.ts"],
  bundle:      true,
  format:      "cjs",
  outfile:     "dist/index.cjs",
  target:      "es2020",
  platform:    "neutral",
  sourcemap:   true,
  external,
})

// Subpath: build/abi (the CE AbiModule for @atheory-ai/wasm-plugin-toolkit).
await esbuild.build({
  entryPoints: ["src/build/ce-abi.ts"],
  bundle:      true,
  format:      "esm",
  outfile:     "dist/build/ce-abi.js",
  target:      "es2020",
  platform:    "neutral",
  sourcemap:   true,
  external,
})
