#!/bin/sh
set -eu

OWNER="atheory-ai"
REPO="context-engine"
BIN_NAME="ce"
INSTALL_DIR="${CE_INSTALL_DIR:-${INSTALL_DIR:-$HOME/.local/bin}}"
VERSION="${CE_VERSION:-latest}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "install.sh: required command not found: $1" >&2
    exit 1
  fi
}

detect_os() {
  case "$(uname -s)" in
    Darwin) echo "darwin" ;;
    Linux) echo "linux" ;;
    *) echo "install.sh: unsupported OS: $(uname -s)" >&2; exit 1 ;;
  esac
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) echo "install.sh: unsupported architecture: $(uname -m)" >&2; exit 1 ;;
  esac
}

download() {
  url="$1"
  out="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$out"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$out"
  else
    echo "install.sh: curl or wget is required" >&2
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
    echo "install.sh: curl or wget is required" >&2
    exit 1
  fi
}

verify_checksum() {
  checksum_file="$1"
  archive_file="$2"
  archive_name="$(basename "$archive_file")"

  if [ ! -s "$checksum_file" ]; then
    echo "install.sh: checksums not found; skipping checksum verification" >&2
    return 0
  fi

  expected="$(grep "  $archive_name\$" "$checksum_file" | awk '{print $1}' || true)"
  if [ -z "$expected" ]; then
    echo "install.sh: checksum for $archive_name not found; skipping checksum verification" >&2
    return 0
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$archive_file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$archive_file" | awk '{print $1}')"
  else
    echo "install.sh: sha256sum or shasum not found; skipping checksum verification" >&2
    return 0
  fi

  if [ "$actual" != "$expected" ]; then
    echo "install.sh: checksum mismatch for $archive_name" >&2
    exit 1
  fi
}

need tar

os="$(detect_os)"
arch="$(detect_arch)"

if [ "$VERSION" = "latest" ]; then
  VERSION="$(latest_version)"
fi

if [ -z "$VERSION" ]; then
  echo "install.sh: could not resolve release version" >&2
  exit 1
fi

tmp="$(mktemp -d "${TMPDIR:-/tmp}/ce-install.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT

bundle="ce-$VERSION-dist.tar.gz"
bundle_url="https://github.com/$OWNER/$REPO/releases/download/v$VERSION/$bundle"

echo "Downloading $bundle_url"
download "$bundle_url" "$tmp/$bundle"

tar -xzf "$tmp/$bundle" -C "$tmp"

archive="$(find "$tmp/dist" -type f -name "ce_*_${os}_${arch}.tar.gz" | head -n 1)"
if [ -z "$archive" ]; then
  echo "install.sh: could not find archive for ${os}/${arch} in release bundle" >&2
  exit 1
fi

checksums="$(find "$tmp/dist" -type f -name "checksums.txt" | head -n 1 || true)"
if [ -n "$checksums" ]; then
  verify_checksum "$checksums" "$archive"
fi

mkdir -p "$tmp/extract" "$INSTALL_DIR"
tar -xzf "$archive" -C "$tmp/extract"

binary="$(find "$tmp/extract" -type f -name "$BIN_NAME" | head -n 1)"
if [ -z "$binary" ]; then
  echo "install.sh: $BIN_NAME binary not found in archive" >&2
  exit 1
fi

cp "$binary" "$INSTALL_DIR/$BIN_NAME"
chmod +x "$INSTALL_DIR/$BIN_NAME"

echo "Installed $BIN_NAME $VERSION to $INSTALL_DIR/$BIN_NAME"
echo "Make sure $INSTALL_DIR is on your PATH."
