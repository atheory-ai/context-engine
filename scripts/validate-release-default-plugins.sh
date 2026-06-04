#!/usr/bin/env bash
set -euo pipefail

required_plugins=(
  go-language.wasm
  go-grammar.wasm
  typescript.wasm
  typescript-grammar.wasm
  python.wasm
  python-grammar.wasm
  php.wasm
  wordpress-conventions.wasm
  woocommerce-conventions.wasm
)

goreleaser_bin="${GORELEASER:-goreleaser}"

if ! command -v "${goreleaser_bin}" >/dev/null 2>&1; then
  echo "goreleaser is required for release default plugin validation" >&2
  exit 127
fi

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
defaults_dir="${repo_root}/internal/indexer/defaults"
work_dir="$(mktemp -d "${TMPDIR:-/tmp}/ce-release-defaults.XXXXXX")"

cleanup() {
  for name in "${required_plugins[@]}"; do
    rm -f "${defaults_dir}/${name}"
  done
  rm -rf "${work_dir}"
}
trap cleanup EXIT

cd "${repo_root}"
rm -rf dist

for name in "${required_plugins[@]}"; do
  printf 'ce-release-default:%s\n' "${name}" >"${defaults_dir}/${name}"
done

"${goreleaser_bin}" build --snapshot --clean --single-target

binary="$(find "${repo_root}/dist" -type f \( -name ce -o -name ce.exe \) | head -n 1)"
if [[ -z "${binary}" ]]; then
  echo "release dry run did not produce a ce binary under dist/" >&2
  exit 1
fi
chmod +x "${binary}" 2>/dev/null || true

project_dir="${work_dir}/project"
data_dir="${work_dir}/data"
mkdir -p "${project_dir}"
cat >"${project_dir}/ce.yaml" <<'YAML'
project:
  git_url: https://example.invalid/context-engine-release-validation.git
  base_prompt: Release validation project.
  arch_prompt: Used only to trigger embedded default extraction.
llm:
  provider: local
engine:
  max_loops: 1
YAML

set +e
"${binary}" --data-dir "${data_dir}" --config "${project_dir}/ce.yaml" query "trigger default extraction" \
  >"${work_dir}/ce.stdout" 2>"${work_dir}/ce.stderr"
run_status=$?
set -e

if [[ "${run_status}" -eq 0 ]]; then
  echo "release dry-run query unexpectedly succeeded; validation expected only extraction" >&2
  exit 1
fi

extracted_dir="${data_dir}/plugins/defaults"
for name in "${required_plugins[@]}"; do
  extracted="${extracted_dir}/${name}"
  if [[ ! -f "${extracted}" ]]; then
    echo "missing extracted default plugin: ${name}" >&2
    echo "--- ce stdout ---" >&2
    cat "${work_dir}/ce.stdout" >&2
    echo "--- ce stderr ---" >&2
    cat "${work_dir}/ce.stderr" >&2
    exit 1
  fi

  expected="ce-release-default:${name}"
  actual="$(tr -d '\r\n' <"${extracted}")"
  if [[ "${actual}" != "${expected}" ]]; then
    echo "extracted default plugin ${name} did not match embedded release marker" >&2
    exit 1
  fi
done

echo "Release dry-run bundled default plugin validation passed."
