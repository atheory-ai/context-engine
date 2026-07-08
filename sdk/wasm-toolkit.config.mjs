// Root config consumed by every plugin's `wasm-toolkit-build` invocation.
// Each plugin/<name>/package.json points at this file via
// `--config ../../wasm-toolkit.config.mjs`.
//
// When the AbiModule evolves (e.g. when CE migrates to a WIT-coupled ABI),
// edit `@atheory-ai/ce-plugin-sdk/build/abi` — every plugin picks up the
// change on the next build with no per-plugin changes needed.

import abi from "@atheory-ai/ce-plugin-sdk/build/abi"

export default { abi }
