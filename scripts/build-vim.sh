#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${ROOT}/dist"
SOURCE="${ROOT}/ide/vim"

mkdir -p "${DIST}"
rm -f "${DIST}/go-doc-vim.zip"

(
  cd "${SOURCE}"
  zip -qr "${DIST}/go-doc-vim.zip" .
)
