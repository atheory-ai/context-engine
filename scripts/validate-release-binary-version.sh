#!/usr/bin/env sh
# Verify that an executable release archive reports the version represented by
# its tag. This guards the linker target used by .goreleaser.yaml.
set -eu

version=${1:?usage: validate-release-binary-version.sh <version> <archive>}
archive=${2:?usage: validate-release-binary-version.sh <version> <archive>}

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

tar -xzf "$archive" -C "$tmpdir"
actual=$(
  "$tmpdir/ce" version | sed -n '1p'
)
expected="ce version $version"

if [ "$actual" != "$expected" ]; then
  echo "release binary reports the wrong version" >&2
  echo "expected: $expected" >&2
  echo "actual:   $actual" >&2
  exit 1
fi
