#!/usr/bin/env bash
# Rebuild rust.wasm — the FOREIGN (non-bundled) grammar fixture used by
# TestRegisterForeignGrammar to prove runtime-loadable grammar plugins.
#
# Rust is deliberately NOT one of the engine's bundled grammars
# (go/python/javascript/typescript/tsx) and carries its own external scanner, so
# this fixture exercises the full plugin-provided-grammar path — foreign node
# types + a foreign scanner (GOT.mem / libc imports) — that the bundled grammars
# can't. Built the same way as the embedded grammars (see ../wasm/build.sh).
#
# Requires zig 0.13.x + go on PATH. Reproducible: sources come from the pinned
# malivvan/tree-sitter module in the Go module cache.
#
# Usage:  ./build-rust.sh
set -euo pipefail

SRC="$(go env GOMODCACHE)/github.com/malivvan/tree-sitter@v0.0.1/src"
[ -d "$SRC/rust" ] || { echo "rust sources not found at $SRC/rust (run: go mod download github.com/malivvan/tree-sitter@v0.0.1)"; exit 1; }
command -v zig >/dev/null || { echo "zig not found (need 0.13.x)"; exit 1; }

HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="$(mktemp -d)"; trap 'rm -rf "$WORK"' EXIT
zig_cc() { zig cc --target=wasm32-wasi-musl "$@"; }

zig_cc -fPIC -O2 -I "$SRC/rust" -I "$SRC" -c "$SRC/rust/parser.c"  -o "$WORK/rust_p.o"
zig_cc -fPIC -O2 -I "$SRC/rust" -I "$SRC" -c "$SRC/rust/scanner.c" -o "$WORK/rust_s.o"
zig wasm-ld --experimental-pic -shared --no-entry --strip-debug \
  --export=tree_sitter_rust --allow-undefined \
  "$WORK/rust_p.o" "$WORK/rust_s.o" -o "$HERE/rust.wasm"

echo "built $HERE/rust.wasm"
shasum -a 256 "$HERE/rust.wasm"
