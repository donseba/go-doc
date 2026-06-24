#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="${ROOT}/dist"

rm -rf "${DIST}"
mkdir -p "${DIST}"

cd "${ROOT}"
go test ./...

GOOS=windows GOARCH=amd64 go build -o "${DIST}/go-doc_windows_amd64.exe" .
GOOS=darwin GOARCH=amd64 go build -o "${DIST}/go-doc_darwin_amd64" .
GOOS=darwin GOARCH=arm64 go build -o "${DIST}/go-doc_darwin_arm64" .

cd "${ROOT}/ide/goland"
gradle buildPlugin
GOLAND_VERSION="$(gradle -q properties | awk -F': ' '/^version:/ {print $2; exit}')"
cp "build/distributions/go-doc-goland-plugin-${GOLAND_VERSION}.zip" "${DIST}/"

cd "${ROOT}/ide/vscode"
node --check extension.js
npx --yes @vscode/vsce package --out "${DIST}/go-doc-vscode-$(node -p "require('./package.json').version").vsix"

cd "${DIST}"
find . -maxdepth 1 -type f ! -name SHA256SUMS.txt -print0 |
  sort -z |
  xargs -0 sha256sum > SHA256SUMS.txt
