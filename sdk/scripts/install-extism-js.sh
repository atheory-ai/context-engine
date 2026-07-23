#!/usr/bin/env sh
# Install pinned, release-built tools required by ce-plugin-build. The caller
# supplies a destination directory; its bin/ directory can be added to PATH.
set -eu

dest=${1:?usage: install-extism-js.sh <destination>}
extism_version=v1.5.1
binaryen_version=version_131

case "$(uname -s)-$(uname -m)" in
  Linux-x86_64) extism_target=x86_64-linux; binaryen_target=x86_64-linux ;;
  Linux-aarch64|Linux-arm64) extism_target=aarch64-linux; binaryen_target=aarch64-linux ;;
  Darwin-x86_64) extism_target=x86_64-macos; binaryen_target=x86_64-macos ;;
  Darwin-arm64) extism_target=aarch64-macos; binaryen_target=arm64-macos ;;
  *) echo "unsupported platform: $(uname -s)-$(uname -m)" >&2; exit 1 ;;
esac

mkdir -p "$dest/bin"
tmp=$(mktemp -d "${TMPDIR:-/tmp}/ce-extism-tools.XXXXXX")
trap 'rm -rf "$tmp"' EXIT

curl -fsSL "https://github.com/extism/js-pdk/releases/download/${extism_version}/extism-js-${extism_target}-${extism_version}.gz" \
  | gzip -dc >"$dest/bin/extism-js"
chmod +x "$dest/bin/extism-js"

curl -fsSL "https://github.com/WebAssembly/binaryen/releases/download/${binaryen_version}/binaryen-${binaryen_version}-${binaryen_target}.tar.gz" \
  | tar -xz -C "$tmp"
binaryen_merge=$(find "$tmp" -type f -name wasm-merge -print -quit)
if [ -z "$binaryen_merge" ]; then
  echo "Binaryen release did not contain wasm-merge" >&2
  exit 1
fi
binaryen_bin=$(dirname "$binaryen_merge")
cp "$binaryen_bin/wasm-merge" "$binaryen_bin/wasm-opt" "$dest/bin/"
# macOS Binaryen executables link to ../lib/libbinaryen.dylib. Copy the full
# release lib directory next to the project-local binaries; Linux releases may
# be self-contained, but retaining the library directory is harmless and keeps
# the bootstrap portable.
binaryen_root=$(dirname "$binaryen_bin")
if [ -d "$binaryen_root/lib" ]; then
  mkdir -p "$dest/lib"
  cp -R "$binaryen_root/lib/." "$dest/lib/"
fi
echo "$dest/bin"
