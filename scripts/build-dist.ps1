$ErrorActionPreference = "Stop"

$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
$jdkHome = Join-Path $env:USERPROFILE "scoop\apps\temurin17-jdk\current"

if (Test-Path $jdkHome) {
    $env:JAVA_HOME = $jdkHome
    $env:Path = "$env:JAVA_HOME\bin;$env:USERPROFILE\scoop\shims;$env:Path"
}

Remove-Item -Recurse -Force $dist -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force $dist | Out-Null

Push-Location $root
try {
    go test ./...
} finally {
    Pop-Location
}

& (Join-Path $PSScriptRoot "build-goland.ps1")
$golandVersion = ""
Push-Location (Join-Path $root "ide\goland")
try {
    $golandVersion = (gradle -q properties | Select-String "^version:" | Select-Object -First 1).ToString().Split(":")[1].Trim()
} finally {
    Pop-Location
}
Copy-Item -Force (Join-Path $root "ide\goland\build\distributions\go-doc-goland-plugin-$golandVersion.zip") $dist

& (Join-Path $PSScriptRoot "build-vscode.ps1")

Get-ChildItem $dist |
    Where-Object { $_.Name -ne "SHA256SUMS.txt" } |
    Get-FileHash -Algorithm SHA256 |
    ForEach-Object { "$($_.Hash.ToLower())  $(Split-Path $_.Path -Leaf)" } |
    Set-Content -Encoding utf8 (Join-Path $dist "SHA256SUMS.txt")
