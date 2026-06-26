#!/bin/sh
# Install the latest Shepherd release binary.
#   curl -fsSL https://raw.githubusercontent.com/JacobRWebb/shepherd/main/install.sh | sh
# Override the install dir with BINDIR=/usr/local/bin.
set -e

REPO="JacobRWebb/shepherd"
BIN="shepherd"
BINDIR="${BINDIR:-$HOME/.local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac
case "$os" in
  linux | darwin) : ;;
  *) echo "unsupported OS: $os (use install.ps1 on Windows)" >&2; exit 1 ;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" \
  | grep -m1 '"tag_name"' | cut -d '"' -f4)
if [ -z "$tag" ]; then
  echo "could not determine the latest release; is the repo published?" >&2
  exit 1
fi
ver=${tag#v}
url="https://github.com/$REPO/releases/download/${tag}/${BIN}_${ver}_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
echo "Downloading $url"
curl -fsSL "$url" | tar -xz -C "$tmp"

mkdir -p "$BINDIR"
mv "$tmp/$BIN" "$BINDIR/$BIN"
chmod +x "$BINDIR/$BIN"

echo "Installed $BIN $tag to $BINDIR/$BIN"
case ":$PATH:" in
  *":$BINDIR:"*) : ;;
  *) echo "Add $BINDIR to your PATH to run 'shepherd'." ;;
esac
