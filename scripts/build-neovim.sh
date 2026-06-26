#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${ROOT}/dist"
SOURCE="${ROOT}/ide/neovim"
VERSION="$(sed -nE 's/^[[:space:]]*"version"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/p' "${ROOT}/ide/vscode/package.json" | head -n 1)"

if [[ -z "${VERSION}" ]]; then
  echo "could not resolve go-doc package version" >&2
  exit 1
fi

mkdir -p "${DIST}"
rm -f "${DIST}"/go-doc-neovim*.zip

(
  cd "${SOURCE}"
  zip -qr "${DIST}/go-doc-neovim-${VERSION}.zip" .
)
