$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
$source = Join-Path $root "ide\vim"
$version = (Get-Content (Join-Path $root "ide\vscode\package.json") | ConvertFrom-Json).version
if (-not $version) {
    throw "could not resolve go-doc package version"
}
$out = Join-Path $dist "go-doc-vim-$version.zip"

New-Item -ItemType Directory -Force $dist | Out-Null
Get-ChildItem $dist -Filter "go-doc-vim*.zip" -ErrorAction SilentlyContinue | Remove-Item -Force
Compress-Archive -Path (Join-Path $source "*") -DestinationPath $out -Force
