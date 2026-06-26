#!/bin/sh
# Derived from atheory-ai/release-template v0.2.3
# See .release-template-config for substitution values.
#
# Install Context Engine (ce).
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/atheory-ai/context-engine/main/install.sh | sh
#
# Environment:
#   CE_VERSION       Version to install, e.g. 0.1.0 or v0.1.0 (default: latest)
#   CE_INSTALL_DIR   Directory to install into (default: ~/.local/bin)
#   CE_BASE_URL      Override release asset base URL (used by tests)
#
# Options:
#   --dry-run        Print what would be installed without downloading
#   --help           Show this help

set -eu
export LC_ALL=C

OWNER="atheory-ai"
REPO="context-engine"
BIN_NAME="ce"
INSTALL_DIR="${CE_INSTALL_DIR:-${INSTALL_DIR:-$HOME/.local/bin}}"
VERSION="${CE_VERSION:-latest}"
DRY_RUN=0

usage() {
  cat <<'EOF'
Install Context Engine (ce).

Usage:
  curl -fsSL https://raw.githubusercontent.com/atheory-ai/context-engine/main/install.sh | sh

Environment:
  CE_VERSION       Version to install, e.g. 0.1.0 or v0.1.0 (default: latest)
  CE_INSTALL_DIR   Directory to install into (default: ~/.local/bin)
  CE_BASE_URL      Override release asset base URL (used by tests)

Options:
  --dry-run        Print what would be installed without downloading
  --help           Show this help
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --dry-run) DRY_RUN=1 ;;
    --help|-h) usage; exit 0 ;;
    *) echo "ce installer: unknown option: $1" >&2; usage >&2; exit 1 ;;
  esac
  shift
done

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "ce installer: required command not found: $1" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s 2>/dev/null || true)" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) echo "ce installer: unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m 2>/dev/null || true)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) echo "ce installer: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
  esac
}

download() {
  url="$1"; out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$out"
  else
    echo "ce installer: curl or wget is required" >&2
    exit 1
  fi
}

latest_version() {
  if command -v curl >/dev/null 2>&1; then
    curl -fsSLI -o /dev/null -w "%{url_effective}" "https://github.com/$OWNER/$REPO/releases/latest" |
      sed 's#.*/tag/v##'
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "https://github.com/$OWNER/$REPO/releases/latest" |
      sed -n 's#.*href="/'"$OWNER"'/'"$REPO"'/releases/tag/v\([^"]*\)".*#\1#p' |
      head -n 1
  else
    echo "ce installer: curl or wget is required" >&2
    exit 1
  fi
}

verify_checksum() {
  checksum_file="$1"; archive_file="$2"
  archive_name="$(basename "$archive_file")"
  expected="$(grep "  $archive_name\$" "$checksum_file" | awk '{print $1}' || true)"
  if [ -z "$expected" ]; then
    echo "ce installer: checksum for $archive_name not found" >&2; exit 1
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive_file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$archive_file" | awk '{print $1}')"
  else
    echo "ce installer: sha256sum or shasum is required" >&2; exit 1
  fi
  if [ "$actual" != "$expected" ]; then
    echo "ce installer: checksum mismatch for $archive_name" >&2; exit 1
  fi
}

# Cosign verification (skipped if cosign is not on PATH — see docs/COSIGN.md
# for why we don't auto-install).
#
# Validates that the archive was signed by the project's release
# workflow running on a tagged commit, via keyless OIDC against the
# Sigstore Fulcio CA and Rekor transparency log.
#
# To require verification, install cosign before running this script:
#   brew install cosign       # macOS
#   apt-get install cosign    # Debian/Ubuntu
# Or set CE_REQUIRE_COSIGN=1 to fail if cosign is missing.
verify_cosign() {
  archive_file="$1"
  archive_name="$(basename "$archive_file")"

  if ! command -v cosign >/dev/null 2>&1; then
    if [ "${CE_REQUIRE_COSIGN:-0}" = "1" ]; then
      echo "ce installer: cosign required (set CE_REQUIRE_COSIGN=0 to skip) but not installed" >&2
      exit 1
    fi
    return 0
  fi

  bundle_url="$base_url/${archive_name}.bundle"
  bundle="$tmp/${archive_name}.bundle"

  if ! download "$bundle_url" "$bundle" 2>/dev/null; then
    if [ "${CE_REQUIRE_COSIGN:-0}" = "1" ]; then
      echo "ce installer: cosign bundle required but not found at $bundle_url" >&2
      exit 1
    fi
    echo "ce installer: cosign bundle not found at $bundle_url" >&2
    echo "  (older releases predate cosign signing; skipping)" >&2
    return 0
  fi

  cert_identity_regex="^https://github.com/$OWNER/$REPO/.github/workflows/release(-full)?\\.yml@refs/tags/v.+\$"
  oidc_issuer="https://token.actions.githubusercontent.com"

  if ! cosign verify-blob \
        --bundle "$bundle" \
        --certificate-identity-regexp "$cert_identity_regex" \
        --certificate-oidc-issuer "$oidc_issuer" \
        "$archive_file" >/dev/null 2>&1; then
    echo "ce installer: cosign verification FAILED for $archive_name" >&2
    echo "  Expected signer identity (regex): $cert_identity_regex" >&2
    echo "  Expected OIDC issuer:             $oidc_issuer" >&2
    exit 1
  fi

  echo "Cosign signature verified: $archive_name"
}

os="$(detect_os)"
arch="$(detect_arch)"

if [ "$VERSION" = "latest" ]; then
  VERSION="$(latest_version)"
fi
case "$VERSION" in v*) VERSION="${VERSION#v}" ;; esac
if [ -z "$VERSION" ]; then
  echo "ce installer: could not resolve release version" >&2; exit 1
fi

case "$os" in
  darwin|linux)
    archive_name="ce_${VERSION}_${os}_${arch}.tar.gz"
    binary_name="$BIN_NAME"
    ;;
  windows)
    archive_name="ce_${VERSION}_${os}_${arch}.zip"
    binary_name="$BIN_NAME.exe"
    ;;
esac

if [ "${CE_BASE_URL:-}" ]; then
  base_url="${CE_BASE_URL%/}"
else
  base_url="https://github.com/$OWNER/$REPO/releases/download/v$VERSION"
fi

echo "Installing ce $VERSION for ${os}/${arch}"
echo "Asset: $archive_name"
echo "Install dir: $INSTALL_DIR"

if [ "$DRY_RUN" = "1" ]; then
  echo "Dry run: would download $base_url/$archive_name"
  exit 0
fi

tmp="$(mktemp -d "${TMPDIR:-/tmp}/ce-install.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT INT TERM

archive="$tmp/$archive_name"
checksums="$tmp/checksums.txt"

download "$base_url/$archive_name" "$archive"
download "$base_url/checksums.txt" "$checksums"
verify_checksum "$checksums" "$archive"
verify_cosign "$archive"

mkdir -p "$tmp/extract"
case "$archive_name" in
  *.tar.gz) need tar; tar -xzf "$archive" -C "$tmp/extract" ;;
  *.zip)    need unzip; unzip -q "$archive" -d "$tmp/extract" ;;
esac

binary="$(find "$tmp/extract" -type f -name "$binary_name" | head -n 1)"
if [ -z "$binary" ]; then
  echo "ce installer: $binary_name binary not found in archive" >&2; exit 1
fi

mkdir -p "$INSTALL_DIR"
install_path="$INSTALL_DIR/$binary_name"
cp "$binary" "$install_path"
chmod 755 "$install_path"

echo "Installed ce $VERSION to $install_path"

case ":$PATH:" in
  *":$INSTALL_DIR:"*) ;;
  *)
    echo ""
    echo "Note: $INSTALL_DIR is not on PATH."
    echo "Add it to your shell profile, for example:"
    echo "  export PATH=\"$INSTALL_DIR:\$PATH\""
    ;;
esac
