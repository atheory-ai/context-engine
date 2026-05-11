# Plugin Runtime Fixtures

Runtime WASM fixtures are generated in `runtime_test.go` so tests do not depend
on a sibling repository, Node toolchain, TypeScript SDK build, or WAT compiler.

The generated fixtures cover valid Extism output, invalid manifest output,
missing exports, invalid bytes, and a raw manifest export that exercises wazero
validation independently of Extism's ABI.
