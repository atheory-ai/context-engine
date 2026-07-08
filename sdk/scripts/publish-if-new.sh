#!/usr/bin/env bash
# Publish a workspace package unless that name@version is already on npm.
# Makes the release job idempotent: a re-run after a partial failure skips the
# packages already published instead of erroring on a duplicate version.
set -euo pipefail
pkg_dir="$1"   # e.g. packages/plugin-sdk (relative to sdk/)
name="$2"      # e.g. @atheory-ai/ce-plugin-sdk
version="$3"

if npm view "${name}@${version}" version >/dev/null 2>&1; then
  echo "${name}@${version} already published — skipping"
  exit 0
fi
pnpm --dir "${pkg_dir}" publish --access public --provenance --no-git-checks
