// CE plugin AbiModule for @atheory-ai/wasm-plugin-toolkit.
//
// Re-exported as `@atheory-ai/ce-plugin-sdk/build/abi` via package.json
// exports so plugins can `import abi from "@atheory-ai/ce-plugin-sdk/build/abi"`
// in their `wasm-toolkit.config.mjs`.

import type { AbiModule } from "@atheory-ai/wasm-plugin-toolkit"

type CEAbiModule = AbiModule & {
  readonly callConvention: "javy-stream-io"
}

export const CE_PLUGIN_ABI: CEAbiModule = {
  name: "ce-plugin",
  abiVersion: 1,
  callConvention: "javy-stream-io",
  wit: `package atheory:ce;
world plugin {
  export ce-plugin-manifest: func();
  export ce-language-match: func();
  export ce-language-extract: func();
  export ce-language-concepts: func();
  export ce-analyzers-list: func();
  export ce-analyzer-run: func();
  export ce-tools-list: func();
  export ce-tool-activate: func();
  export ce-tool-execute: func();
}
`,
  entryWrapper: ({ userEntryRelative }) => `
import plugin from "${userEntryRelative}"
import { definePlugin } from "@atheory-ai/ce-plugin-sdk"
import {
  cePluginManifest,
  ceLanguageMatch,
  ceLanguageExtract,
  ceLanguageConcepts,
  ceAnalyzersList,
  ceAnalyzerRun,
  ceToolsList,
  ceToolActivate,
  ceToolExecute,
} from "@atheory-ai/ce-plugin-sdk/abi"

definePlugin(plugin)

export {
  cePluginManifest,
  ceLanguageMatch,
  ceLanguageExtract,
  ceLanguageConcepts,
  ceAnalyzersList,
  ceAnalyzerRun,
  ceToolsList,
  ceToolActivate,
  ceToolExecute,
}
`,
  requiredExports: [
    "ce-plugin-manifest",
    "ce-language-match",
    "ce-language-extract",
    "ce-language-concepts",
    "ce-analyzers-list",
    "ce-analyzer-run",
    "ce-tools-list",
    "ce-tool-activate",
    "ce-tool-execute",
  ],
}

export default CE_PLUGIN_ABI
