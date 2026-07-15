#!/usr/bin/env bash
# Rebuild php.wasm, the ABI-compatible PHP grammar fixture for the external
# grammar runtime test. Sources are the same pinned corpus as CE's core.
set -euo pipefail
export LANG=C
export LC_ALL=C

ZIG="${ZIG:-zig}"
case "$("$ZIG" version)" in
  0.13.*) ;;
  *) echo "zig 0.13.x is required; set ZIG=/path/to/zig-0.13" >&2; exit 1 ;;
esac

SRC="$(go env GOMODCACHE)/github.com/malivvan/tree-sitter@v0.0.1/src"
test -f "$SRC/php/parser.c"
test -f "$SRC/php/scanner.c"
HERE="$(cd "$(dirname "$0")" && pwd)"
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

"$ZIG" cc --target=wasm32-wasi-musl -fPIC -O2 -I "$SRC/php" -I "$SRC" -c "$SRC/php/parser.c" -o "$WORK/php_p.o"
"$ZIG" cc --target=wasm32-wasi-musl -fPIC -O2 -I "$SRC/php" -I "$SRC" -c "$SRC/php/scanner.c" -o "$WORK/php_s.o"
"$ZIG" wasm-ld --experimental-pic -shared --no-entry --strip-debug \
  --export=tree_sitter_php --allow-undefined \
  "$WORK/php_p.o" "$WORK/php_s.o" -o "$HERE/php.wasm"

shasum -a 256 "$HERE/php.wasm"
