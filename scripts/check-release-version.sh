#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TAG="${1:-${CIRCLE_TAG:-}}"

if [[ -z "${TAG}" ]]; then
  echo "release tag is required, for example v0.2.5" >&2
  exit 1
fi

VERSION="${TAG#v}"

GOLAND_VERSION="$(
  cd "${ROOT}/ide/goland"
  gradle -q properties | awk -F': ' '/^version:/ {print $2; exit}'
)"
VSCODE_VERSION="$(cd "${ROOT}/ide/vscode" && node -p "require('./package.json').version")"
SUBLIME_VERSION="$(cd "${ROOT}" && python -c 'import json, pathlib; print(json.loads(pathlib.Path("ide/sublime/sublime-package.json").read_text())["version"])' 2>/dev/null || python3 -c 'import json, pathlib; print(json.loads(pathlib.Path("ide/sublime/sublime-package.json").read_text())["version"])')"

if [[ "${GOLAND_VERSION}" != "${VERSION}" ]]; then
  echo "GoLand plugin version ${GOLAND_VERSION} does not match tag ${TAG}" >&2
  exit 1
fi

if [[ "${VSCODE_VERSION}" != "${VERSION}" ]]; then
  echo "VS Code extension version ${VSCODE_VERSION} does not match tag ${TAG}" >&2
  exit 1
fi

if [[ "${SUBLIME_VERSION}" != "${VERSION}" ]]; then
  echo "Sublime Text package version ${SUBLIME_VERSION} does not match tag ${TAG}" >&2
  exit 1
fi

echo "release version ${VERSION} matches ${TAG}"
