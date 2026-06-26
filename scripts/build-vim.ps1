$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
$source = Join-Path $root "ide\vim"
$versionLine = Select-String -Path (Join-Path $root "internal\godoccli\version.go") -Pattern '^const Version = "([^"]+)"' | Select-Object -First 1
if (-not $versionLine) {
    throw "could not resolve go-doc version"
}
$version = $versionLine.Matches[0].Groups[1].Value
$out = Join-Path $dist "go-doc-vim-$version.zip"

New-Item -ItemType Directory -Force $dist | Out-Null
Get-ChildItem $dist -Filter "go-doc-vim*.zip" -ErrorAction SilentlyContinue | Remove-Item -Force
Compress-Archive -Path (Join-Path $source "*") -DestinationPath $out -Force
