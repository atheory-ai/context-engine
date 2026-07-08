import * as esbuild from "esbuild"

await esbuild.build({
  entryPoints: ["src/index.ts"],
  bundle:      true,
  format:      "esm",
  outfile:     "dist/index.js",
  target:      "node18",
  platform:    "node",
  sourcemap:   true,
  banner: {
    js: "#!/usr/bin/env node",
  },
})
