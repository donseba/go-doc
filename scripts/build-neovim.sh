#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${ROOT}/dist"
SOURCE="${ROOT}/ide/neovim"

mkdir -p "${DIST}"
rm -f "${DIST}/go-doc-neovim.zip"

(
  cd "${SOURCE}"
  zip -qr "${DIST}/go-doc-neovim.zip" .
)
