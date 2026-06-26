#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${ROOT}/dist"
SOURCE="${ROOT}/ide/sublime"

mkdir -p "${DIST}"
rm -f "${DIST}/go-doc-sublime.sublime-package" "${DIST}/LSP-go-doc.sublime-package"

(
  cd "${SOURCE}"
  zip -qr "${DIST}/go-doc-sublime.sublime-package" .
)
