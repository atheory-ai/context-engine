# Embedded tree-sitter WASM — provenance

These WASM blobs are vendored (committed) and embedded into the `ce` binary via
`//go:embed`. `tree-sitter-core.wasm` is the tree-sitter runtime compiled to
WASM once and reused by every platform build; the rest are grammar side modules
loaded at runtime by the dynamic linker (see
`docs/specs/18-spec-wasm-grammar-loader.md`).

## Sources & toolchain

- **tree-sitter core:** v0.24.7, plus the go/python/javascript/typescript/tsx
  grammar sources — all from the pinned `github.com/malivvan/tree-sitter@v0.0.1`
  module (fetched into the Go module cache).
- **Toolchain:** `zig` 0.13.0 (`zig cc` targeting `wasm32-wasi-musl`; `zig
  wasm-ld` for the PIC grammar side modules).

## Reproducing

```sh
zig ... # ensure zig 0.13.x is on PATH
./build.sh            # regenerate all blobs in place
VERIFY=1 ./build.sh   # rebuild to a temp dir and diff against the committed blobs
```

The build is deterministic — `VERIFY=1` reports every blob byte-identical.

The core explicitly exports the libc functions grammar side modules import,
including `memcmp` for the PHP external scanner. Keep that export list in
`build.sh` aligned with the supported grammar corpus.

## Checksums (sha256)

```
5af5d04e3fe8ab3b2ffb60d1637db091d69b5d69cab143bfc36abeb420117127  go.wasm
19a7099424a44e9cf2bd07ed786ec43e2aa6a4ebe263885c3dc5ce79985f45b5  javascript.wasm
89e514f34cd58e82a04f0e09f5384707126fbc6d3e84ca1ac3cb7965083e967f  python.wasm
bb305679e36fb6e76141772b7e28260d401d3281cae9b4c29794b18c92b47c7e  tree-sitter-core.wasm
6a73353489f9b8def45e5b9213c9697de11852d7534bfb78fe82e7198b040c61  tsx.wasm
757ac11d35b59fae8f54dec004eccbe881e3014110e58cc0b00899fa780922be  typescript.wasm
```

Regenerate this list with `shasum -a 256 *.wasm` after a rebuild.

## Follow-up

A CI workflow (manual-dispatch / version-bump) that runs `build.sh` and commits
the refreshed blobs + checksums is the remaining automation step (spec 18).
