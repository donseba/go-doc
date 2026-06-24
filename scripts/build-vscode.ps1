$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"

New-Item -ItemType Directory -Force $dist | Out-Null

Push-Location (Join-Path $root "ide\vscode")
try {
    npm ci
    node --check extension.js
    $version = node -p "require('./package.json').version"
    npx --yes @vscode/vsce package --out "../../dist/go-doc-vscode-$version.vsix"
} finally {
    Pop-Location
}
