param(
    [string]$Tag = $env:CIRCLE_TAG
)

$ErrorActionPreference = "Stop"

$jdkHome = Join-Path $env:USERPROFILE "scoop\apps\temurin17-jdk\current"
if (Test-Path $jdkHome) {
    $env:JAVA_HOME = $jdkHome
    $env:Path = "$env:JAVA_HOME\bin;$env:USERPROFILE\scoop\shims;$env:Path"
}

if ([string]::IsNullOrWhiteSpace($Tag)) {
    Write-Error "release tag is required, for example v0.2.5"
}

$root = Split-Path -Parent $PSScriptRoot
$version = $Tag.TrimStart("v")

Push-Location (Join-Path $root "ide\goland")
try {
    $golandVersion = (gradle -q properties | Select-String "^version:" | Select-Object -First 1).ToString().Split(":")[1].Trim()
} finally {
    Pop-Location
}

Push-Location (Join-Path $root "ide\vscode")
try {
    $vscodeVersion = node -p "require('./package.json').version"
} finally {
    Pop-Location
}

$sublimeVersion = (Get-Content (Join-Path $root "ide\sublime\sublime-package.json") | ConvertFrom-Json).version
$goDocVersion = (Select-String -Path (Join-Path $root "internal\godoccli\version.go") -Pattern 'const Version = "([^"]+)"').Matches[0].Groups[1].Value

if ($golandVersion -ne $version) {
    Write-Error "GoLand plugin version $golandVersion does not match tag $Tag"
}

if ($vscodeVersion -ne $version) {
    Write-Error "VS Code extension version $vscodeVersion does not match tag $Tag"
}

if ($sublimeVersion -ne $version) {
    Write-Error "Sublime Text package version $sublimeVersion does not match tag $Tag"
}
if ($goDocVersion -ne $version) {
    Write-Error "go-doc CLI version $goDocVersion does not match tag $Tag"
}

Write-Host "release version $version matches $Tag"
