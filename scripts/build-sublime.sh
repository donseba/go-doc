#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${ROOT}/dist"
SOURCE="${ROOT}/ide/sublime"
VERSION="$(sed -nE 's/^const Version = "([^"]+)"/\1/p' "${ROOT}/internal/godoccli/version.go")"

if [[ -z "${VERSION}" ]]; then
  echo "could not resolve go-doc version" >&2
  exit 1
fi

mkdir -p "${DIST}"
rm -f "${DIST}"/go-doc-sublime*.sublime-package "${DIST}/LSP-go-doc.sublime-package"

(
  cd "${SOURCE}"
  zip -qr "${DIST}/go-doc-sublime-${VERSION}.sublime-package" .
)
