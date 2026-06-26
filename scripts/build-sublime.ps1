$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
$source = Join-Path $root "ide\sublime"
$out = Join-Path $dist "go-doc-sublime.sublime-package"
$zipOut = Join-Path $dist "go-doc-sublime.zip"

New-Item -ItemType Directory -Force $dist | Out-Null
Remove-Item -Force $out -ErrorAction SilentlyContinue
Remove-Item -Force $zipOut -ErrorAction SilentlyContinue
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
