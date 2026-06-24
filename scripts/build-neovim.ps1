$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
$source = Join-Path $root "ide\neovim"
$out = Join-Path $dist "go-doc-neovim.zip"

New-Item -ItemType Directory -Force $dist | Out-Null
Remove-Item -Force $out -ErrorAction SilentlyContinue
Compress-Archive -Path (Join-Path $source "*") -DestinationPath $out -Force
