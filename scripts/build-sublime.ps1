$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
$source = Join-Path $root "ide\sublime"
$versionLine = Select-String -Path (Join-Path $root "internal\godoccli\version.go") -Pattern '^const Version = "([^"]+)"' | Select-Object -First 1
if (-not $versionLine) {
    throw "could not resolve go-doc version"
}
$version = $versionLine.Matches[0].Groups[1].Value
$out = Join-Path $dist "go-doc-sublime-$version.sublime-package"
$zipOut = Join-Path $dist "go-doc-sublime-$version.zip"

New-Item -ItemType Directory -Force $dist | Out-Null
Get-ChildItem $dist -Filter "go-doc-sublime*.sublime-package" -ErrorAction SilentlyContinue | Remove-Item -Force
Get-ChildItem $dist -Filter "go-doc-sublime*.zip" -ErrorAction SilentlyContinue | Remove-Item -Force
Remove-Item -Force (Join-Path $dist "LSP-go-doc.sublime-package") -ErrorAction SilentlyContinue
Remove-Item -Force (Join-Path $dist "LSP-go-doc.zip") -ErrorAction SilentlyContinue

$temp = Join-Path ([System.IO.Path]::GetTempPath()) ("go-doc-sublime-" + [System.Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Force $temp | Out-Null

try {
    Copy-Item -Recurse -Force (Join-Path $source "*") $temp
    Compress-Archive -Path (Join-Path $temp "*") -DestinationPath $zipOut -Force
    Move-Item -Force $zipOut $out
} finally {
    Remove-Item -Recurse -Force $temp -ErrorAction SilentlyContinue
}
