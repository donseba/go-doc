#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TAG="${1:-${CIRCLE_TAG:-}}"

if [[ -z "${TAG}" ]]; then
  echo "release tag is required, for example v0.2.5" >&2
  exit 1
fi

VERSION="${TAG#v}"

if [[ ! "${VERSION}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "release tag must look like vMAJOR.MINOR.PATCH; got ${TAG}" >&2
  exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
  echo "jq is required to set release versions" >&2
  exit 1
fi

sed -i.bak -E "s/^version[[:space:]]*=[[:space:]]*\"[^\"]+\"/version = \"${VERSION}\"/" "${ROOT}/ide/goland/build.gradle.kts"
rm -f "${ROOT}/ide/goland/build.gradle.kts.bak"

tmp="$(mktemp)"
jq --arg version "${VERSION}" '.version = $version' "${ROOT}/ide/vscode/package.json" > "${tmp}"
mv "${tmp}" "${ROOT}/ide/vscode/package.json"

tmp="$(mktemp)"
jq --arg version "${VERSION}" '.version = $version | .packages[""].version = $version' "${ROOT}/ide/vscode/package-lock.json" > "${tmp}"
mv "${tmp}" "${ROOT}/ide/vscode/package-lock.json"

tmp="$(mktemp)"
jq --arg version "${VERSION}" '.version = $version' "${ROOT}/ide/sublime/sublime-package.json" > "${tmp}"
mv "${tmp}" "${ROOT}/ide/sublime/sublime-package.json"

echo "release manifests set to ${VERSION}"
