#!/usr/bin/env sh
set -eu

OWNER="${GITPLEX_OWNER:-abhishek-yadav-aps}"
REPO="${GITPLEX_REPO:-gitplex}"
VERSION="${GITPLEX_VERSION:-latest}"
INSTALL_DIR="${GITPLEX_INSTALL_DIR:-/usr/local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  darwin|linux) ;;
  *)
    echo "gitplex: unsupported OS: $os" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *)
    echo "gitplex: unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

if [ "$VERSION" = "latest" ]; then
  url="https://github.com/${OWNER}/${REPO}/releases/latest/download/gitplex_${os}_${arch}.tar.gz"
else
  url="https://github.com/${OWNER}/${REPO}/releases/download/${VERSION}/gitplex_${os}_${arch}.tar.gz"
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

echo "Downloading gitplex from $url"
curl -fsSL "$url" -o "$tmpdir/gitplex.tar.gz"
tar -xzf "$tmpdir/gitplex.tar.gz" -C "$tmpdir"

mkdir -p "$INSTALL_DIR"
if [ -w "$INSTALL_DIR" ]; then
  mv "$tmpdir/gitplex" "$INSTALL_DIR/gitplex"
else
  sudo mv "$tmpdir/gitplex" "$INSTALL_DIR/gitplex"
fi

echo "Installed gitplex to $INSTALL_DIR/gitplex"
